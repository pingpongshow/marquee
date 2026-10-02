// Renders Marquee's app icons (iOS single-size icon, tvOS layered icon and Top Shelf).
// Usage: swift scripts/make-icons.swift <output dir>
import AppKit

let gold = NSColor(calibratedRed: 0.96, green: 0.74, blue: 0.27, alpha: 1)
let bgTop = NSColor(calibratedRed: 0.13, green: 0.12, blue: 0.17, alpha: 1)
let bgBottom = NSColor(calibratedRed: 0.05, green: 0.05, blue: 0.08, alpha: 1)

func render(_ w: Int, _ h: Int, _ draw: (CGRect) -> Void) -> Data {
    let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: w, pixelsHigh: h, bitsPerSample: 8, samplesPerPixel: 4,
                               hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    draw(CGRect(x: 0, y: 0, width: w, height: h))
    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:])!
}

func background(_ r: CGRect) {
    NSGradient(starting: bgTop, ending: bgBottom)!.draw(in: r, angle: -90)
}

/// The marquee sign: a gold-bordered panel ringed with bulbs, with a big "M".
func sign(_ r: CGRect, scale: CGFloat = 0.62) {
    let side = min(r.width, r.height) * scale
    let panel = CGRect(x: r.midX - side * 0.6, y: r.midY - side / 2, width: side * 1.2, height: side)
    let path = NSBezierPath(roundedRect: panel, xRadius: side * 0.08, yRadius: side * 0.08)
    NSColor(calibratedRed: 0.09, green: 0.08, blue: 0.11, alpha: 1).setFill()
    path.fill()
    gold.setStroke()
    path.lineWidth = side * 0.03
    path.stroke()
    // Bulbs along the border.
    let bulb = side * 0.045
    let inset = side * 0.075
    let inner = panel.insetBy(dx: inset, dy: inset)
    let nx = 9, ny = 7
    var points: [CGPoint] = []
    for i in 0..<nx { let x = inner.minX + inner.width * CGFloat(i) / CGFloat(nx - 1); points += [CGPoint(x: x, y: inner.minY), CGPoint(x: x, y: inner.maxY)] }
    for j in 1..<(ny - 1) { let y = inner.minY + inner.height * CGFloat(j) / CGFloat(ny - 1); points += [CGPoint(x: inner.minX, y: y), CGPoint(x: inner.maxX, y: y)] }
    for p in points {
        let glow = NSGradient(colors: [gold.withAlphaComponent(0.9), gold.withAlphaComponent(0)])!
        glow.draw(in: NSBezierPath(ovalIn: CGRect(x: p.x - bulb * 1.6, y: p.y - bulb * 1.6, width: bulb * 3.2, height: bulb * 3.2)), relativeCenterPosition: .zero)
        NSColor(calibratedRed: 1, green: 0.93, blue: 0.75, alpha: 1).setFill()
        NSBezierPath(ovalIn: CGRect(x: p.x - bulb / 2, y: p.y - bulb / 2, width: bulb, height: bulb)).fill()
    }
    letter(panel, size: side * 0.62)
}

func letter(_ panel: CGRect, size: CGFloat) {
    let font = NSFont.systemFont(ofSize: size, weight: .black)
    let attrs: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: gold]
    let s = NSAttributedString(string: "M", attributes: attrs)
    let b = s.size()
    s.draw(at: CGPoint(x: panel.midX - b.width / 2, y: panel.midY - b.height / 2 + size * 0.02))
}

let out = URL(fileURLWithPath: CommandLine.arguments[1])
func write(_ data: Data, _ path: String) {
    let url = out.appendingPathComponent(path)
    try! FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
    try! data.write(to: url)
}

// iOS: one 1024 icon.
write(render(1024, 1024) { r in background(r); sign(r) }, "ios-icon-1024.png")
// tvOS layered icon: back (background) and front (sign) layers, small and large.
for (name, w, h) in [("small", 400, 240), ("small2x", 800, 480), ("large", 1280, 768)] {
    write(render(w, h) { r in background(r) }, "tv-\(name)-back.png")
    write(render(w, h) { r in sign(r, scale: 0.8) }, "tv-\(name)-front.png")
}
// Top Shelf banners.
for (name, w, h) in [("topshelf", 1920, 720), ("topshelf-wide", 2320, 720)] {
    write(render(w, h) { r in
        background(r)
        sign(CGRect(x: 0, y: 0, width: h, height: h).offsetBy(dx: r.width * 0.12, dy: 0), scale: 0.7)
        let attrs: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: CGFloat(h) * 0.16, weight: .heavy), .foregroundColor: gold]
        NSAttributedString(string: "MARQUEE", attributes: attrs).draw(at: CGPoint(x: r.width * 0.12 + CGFloat(h) * 1.0, y: r.midY - CGFloat(h) * 0.1))
    }, "\(name).png")
}
print("icons written to \(out.path)")
