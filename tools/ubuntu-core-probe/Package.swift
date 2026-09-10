// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "UbuntuCoreProbe",
    products: [
        .executable(name: "UbuntuCoreProbe", targets: ["UbuntuCoreProbe"]),
    ],
    dependencies: [
        .package(url: "https://github.com/apple/swift-crypto.git", exact: "4.5.2"),
    ],
    targets: [
        .executableTarget(
            name: "UbuntuCoreProbe",
            dependencies: [
                .product(name: "Crypto", package: "swift-crypto"),
            ]
        ),
    ]
)
