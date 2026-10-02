// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "MarqueeKit",
    platforms: [.iOS(.v18), .tvOS(.v18), .macOS(.v15)],
    products: [
        .library(name: "MarqueeKit", targets: ["MarqueeKit"]),
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-openapi-runtime", from: "1.8.0"),
        .package(url: "https://github.com/apple/swift-openapi-urlsession", from: "1.1.0"),
        .package(url: "https://github.com/apple/swift-http-types", from: "1.3.0"),
    ],
    targets: [
        // Generated from api/openapi.yaml by scripts/gen-api.sh. Never edit by hand.
        .target(
            name: "MarqueeAPI",
            dependencies: [.product(name: "OpenAPIRuntime", package: "swift-openapi-runtime")],
            swiftSettings: [.swiftLanguageMode(.v5)]
        ),
        .target(
            name: "MarqueeKit",
            dependencies: [
                "MarqueeAPI",
                .product(name: "OpenAPIRuntime", package: "swift-openapi-runtime"),
                .product(name: "OpenAPIURLSession", package: "swift-openapi-urlsession"),
                .product(name: "HTTPTypes", package: "swift-http-types"),
            ],
            swiftSettings: [.swiftLanguageMode(.v5)]
        ),
        .testTarget(name: "MarqueeKitTests", dependencies: ["MarqueeKit"], swiftSettings: [.swiftLanguageMode(.v5)]),
    ]
)
