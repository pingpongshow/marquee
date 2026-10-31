package soundprint

import (
	"context"
	"regexp"
	"strings"
	"unicode"
)

// Language detection from lyrics, so a mix keeps to the languages its listener plays
// (MUSIC-17). The writing system settles most languages; the ones in Latin letters are told
// apart by their commonest words. A language counts only when the lyrics say it clearly, and
// tracks with no lyrics (instrumentals, or not looked up) have none.

var scripts = []struct {
	lang  string
	table *unicode.RangeTable
}{
	{"ja", unicode.Hiragana}, {"ja", unicode.Katakana}, {"ko", unicode.Hangul}, {"zh", unicode.Han},
	{"ru", unicode.Cyrillic}, {"el", unicode.Greek}, {"ar", unicode.Arabic}, {"he", unicode.Hebrew},
	{"hi", unicode.Devanagari}, {"th", unicode.Thai},
}

// stopwords are each Latin-script language's commonest words, few enough that a word is
// rarely another language's too.
var stopwords = map[string][]string{
	"en": {"the", "and", "you", "that", "is", "it", "with", "for", "this", "my", "your", "me", "what", "love", "be", "are", "was", "don't", "i'm", "when"},
	"es": {"que", "el", "los", "las", "con", "por", "para", "una", "mi", "tu", "yo", "pero", "como", "más", "qué", "corazón", "amor", "está", "eres", "nada"},
	"fr": {"le", "les", "et", "je", "tu", "vous", "nous", "est", "pas", "une", "des", "dans", "pour", "qui", "mon", "toi", "moi", "c'est", "j'ai", "avec"},
	"de": {"und", "ich", "die", "der", "das", "nicht", "ist", "du", "mich", "mir", "ein", "eine", "auf", "wir", "dich", "auch", "noch", "sind", "wie", "kein"},
	"it": {"che", "il", "di", "non", "per", "una", "sono", "mi", "ti", "io", "tu", "della", "nel", "come", "più", "anche", "amore", "questo", "sei", "cosa"},
	"pt": {"que", "não", "eu", "você", "uma", "com", "para", "meu", "minha", "do", "da", "em", "é", "mais", "coração", "amor", "tudo", "sem", "isso", "vou"},
	"nl": {"het", "een", "ik", "je", "niet", "van", "dat", "en", "zijn", "wat", "maar", "mijn", "jij", "voor", "nog", "wel", "naar", "zo", "dan", "hij"},
	"sv": {"och", "jag", "det", "att", "du", "är", "som", "inte", "på", "en", "med", "för", "har", "mig", "dig", "vi", "min", "kan", "så", "när"},
	"pl": {"nie", "się", "jest", "to", "że", "na", "jak", "ja", "ty", "mnie", "co", "już", "tak", "ale", "mi", "czy", "tylko", "jestem", "będzie", "tym"},
	"tr": {"bir", "ve", "bu", "ben", "sen", "ne", "çok", "gibi", "için", "var", "yok", "beni", "seni", "daha", "kadar", "aşk", "değil", "her", "mi", "sana"},
	"id": {"yang", "aku", "kau", "dan", "tak", "ini", "itu", "di", "untuk", "dengan", "cinta", "kita", "akan", "tidak", "ada", "dalam", "hanya", "kamu", "bisa", "ku"},
}

var (
	wordRE    = regexp.MustCompile(`[\pL']+`)
	bracketRE = regexp.MustCompile(`\[[^\]]*\]`)
)

// DetectLanguage returns the language lyrics are written in (ISO 639-1), or "" when they're
// too short or unclear.
func DetectLanguage(lyrics string) string {
	// Timestamps and section markers of synced lyrics aren't words.
	lyrics = bracketRE.ReplaceAllString(lyrics, " ")
	letters := 0
	byScript := map[string]int{}
	for _, r := range lyrics {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		for _, sc := range scripts {
			if unicode.Is(sc.table, r) {
				byScript[sc.lang]++
				break
			}
		}
	}
	if letters < 80 {
		return ""
	}
	// Japanese mixes kanji (Han) with kana: any real share of kana makes it Japanese.
	if byScript["ja"]*10 >= letters {
		return "ja"
	}
	for _, sc := range scripts {
		if byScript[sc.lang]*2 >= letters {
			return sc.lang
		}
	}
	words := wordRE.FindAllString(strings.ToLower(lyrics), -1)
	if len(words) < 25 {
		return ""
	}
	hits := map[string]int{}
	for _, w := range words {
		for lang, list := range stopwords {
			for _, sw := range list {
				if w == sw {
					hits[lang]++
					break
				}
			}
		}
	}
	best, second := "", 0
	for lang, n := range hits {
		if best == "" || n > hits[best] || (n == hits[best] && lang < best) {
			if best != "" {
				second = max(second, hits[best])
			}
			best = lang
		} else {
			second = max(second, n)
		}
	}
	// Clearly that language: a fair share of the words are its common ones, well ahead of the
	// next language.
	if best == "" || hits[best]*100 < len(words)*8 || hits[best] < 2*second {
		return ""
	}
	return best
}

// languagePass sets the language of analysed tracks whose lyrics have been looked up since,
// and keeps it current when lyrics change.
func (s *Service) languagePass(ctx context.Context) (int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT sp.item_id, l.text FROM soundprint sp JOIN lyrics l ON l.item_id = sp.item_id
		WHERE sp.embedding IS NOT NULL AND (sp.language IS NULL OR l.fetched_at > sp.analyzed_at)`)
	if err != nil {
		return 0, err
	}
	type found struct {
		id   int64
		lang string
	}
	var all []found
	for rows.Next() {
		var f found
		var text string
		if rows.Scan(&f.id, &text) == nil {
			f.lang = DetectLanguage(text)
			all = append(all, f)
		}
	}
	rows.Close()
	for _, f := range all {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		// analyzed_at moves on too, so the lyrics aren't read again until they change.
		s.DB.ExecContext(ctx, `UPDATE soundprint SET language = ?, analyzed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE item_id = ?`, f.lang, f.id)
		lang := f.lang
		s.Index.update(f.id, func(t *Track) { t.Language = lang })
	}
	return len(all), nil
}
