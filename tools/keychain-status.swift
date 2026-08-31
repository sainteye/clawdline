import Darwin
import Foundation
import Security

// A noninteractive, single-subject replacement for `security show-keychain-info`. That command
// reports settings, not the kSecUnlockStateStatus bit, and therefore cannot prove signing
// usability. This helper opens only the explicit path it is handed and never changes the search
// list or asks Security to unlock anything.
guard CommandLine.arguments.count == 2 else {
    FileHandle.standardError.write(Data("usage: keychain-status.swift <keychain-path>\n".utf8))
    exit(2)
}

let path = CommandLine.arguments[1]
var keychain: SecKeychain?
let openStatus = SecKeychainOpen(path, &keychain)
guard openStatus == errSecSuccess, let keychain else {
    FileHandle.standardError.write(
        Data("keychain-status: SecKeychainOpen failed with OSStatus \(openStatus)\n".utf8))
    exit(4)
}

var status: SecKeychainStatus = 0
let getStatus = SecKeychainGetStatus(keychain, &status)
guard getStatus == errSecSuccess else {
    FileHandle.standardError.write(
        Data("keychain-status: SecKeychainGetStatus failed with OSStatus \(getStatus)\n".utf8))
    exit(4)
}

guard status & SecKeychainStatus(kSecUnlockStateStatus) != 0 else {
    FileHandle.standardError.write(Data("keychain-status: locked\n".utf8))
    exit(3)
}

print("unlocked")
