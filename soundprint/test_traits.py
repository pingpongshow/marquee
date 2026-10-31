"""Tests for trait grouping (run with: python3 -m unittest test_traits). Needs no model."""

import unittest

from traits import TRAITS, group, label_index


class TraitsTest(unittest.TestCase):
    def test_takes_the_strongest_class_of_each_trait(self):
        labels = {0: "Speech", 1: "Sad music", 2: "Singing", 3: "Female singing", 4: "Piano"}
        index = label_index(labels)
        self.assertEqual(index["sad"], [1])
        self.assertEqual(sorted(index["vocal"]), [2, 3])
        self.assertEqual(index["happy"], [], "a class the model doesn't have is skipped")
        traits = group([0.9, 0.3, 0.2, 0.7, 0.05], index)
        self.assertEqual(traits["sad"], 0.3)
        self.assertEqual(traits["vocal"], 0.7, "the strongest of its classes")
        self.assertEqual(traits["acoustic"], 0.05)
        self.assertNotIn("happy", traits, "no classes, no trait")

    def test_every_trait_has_classes(self):
        for t, names in TRAITS.items():
            self.assertTrue(names, t)


if __name__ == "__main__":
    unittest.main()
