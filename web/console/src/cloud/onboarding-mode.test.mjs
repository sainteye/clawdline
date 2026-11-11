import { test } from "node:test"
import assert from "node:assert/strict"
import { cloudOnboardingMode, isNativeAppShell, isStandaloneWebApp } from "../legacy/js/net/cloud-onboarding.js"

const iphone = (extra = {}) => ({ navigator: { platform: "iPhone", maxTouchPoints: 5 }, matchMedia: () => ({ matches: false }), ...extra })

test("an iPhone browser tab is sent to install the Home Screen app", () => {
  assert.equal(cloudOnboardingMode(iphone()), "install")
})

test("the Home Screen app signs in where it is", () => {
  assert.equal(cloudOnboardingMode(iphone({ navigator: { platform: "iPhone", standalone: true } })), "pwa")
})

test("the iOS app's web view signs in where it is, not through the PWA install", () => {
  const scope = iphone({ clawdlineNative: { platform: "ios", version: "0.1.0" } })
  assert.equal(isNativeAppShell(scope), true)
  assert.equal(isStandaloneWebApp(scope), true)
  assert.equal(cloudOnboardingMode(scope), "pwa")
})

test("only the shell's own announcement counts", () => {
  assert.equal(isNativeAppShell(iphone({ clawdlineNative: true })), false)
  assert.equal(isNativeAppShell(iphone({ clawdlineNative: { platform: "android" } })), false)
  assert.equal(cloudOnboardingMode(iphone({ clawdlineNative: "ios" })), "install")
})

test("a computer stays a browser", () => {
  assert.equal(cloudOnboardingMode({ navigator: { platform: "MacIntel", maxTouchPoints: 0 }, clawdlineNative: { platform: "ios" } }), "browser")
})
