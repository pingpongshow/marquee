// swift-tools-version: 6.0
// Pins the OpenAPI generator used by scripts/gen-api.sh (never edit generated code).
import PackageDescription

let package = Package(
    name: "MarqueeTools",
    platforms: [.macOS(.v14)],
    dependencies: [
        .package(url: "https://github.com/apple/swift-openapi-generator", from: "1.10.0"),
    ],
    targets: []
)
