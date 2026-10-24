#!/usr/bin/env python3
"""Offline evidence tests: actual filesystem/tar inputs, no signing secrets."""
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import subprocess
import sys
import unittest

spec = importlib.util.spec_from_file_location("dmg_evidence", Path(__file__).with_name("dmg-evidence.py"))
dmg = importlib.util.module_from_spec(spec)
spec.loader.exec_module(dmg)


class DeliveryEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        root = Path(self.temp.name)
        self.source, self.delivery, self.tree, self.mount = [root / n for n in ("source", "delivery", "tree", "mount")]
        for p in (self.source, self.delivery, self.tree, self.mount):
            p.mkdir()
        self.version, self.sc, self.pc = "v2.1.2", "2" * 40, "3" * 40
        self.name = f"Clawdline-{self.version}-macos-arm64.tar.gz"
        for p in (self.tree, self.mount):
            app = p / "Clawdline Next.app/Contents"
            app.mkdir(parents=True)
            (app / "Info.plist").write_text("fixture")
            (app / "executable").write_bytes(b"payload")
            (app / "executable").chmod(0o755)
            (app / "link").symlink_to("executable")
        (self.mount / "Applications").symlink_to("/Applications")
        (self.mount / "Install.txt").write_text(dmg.INSTALL)
        with tarfile.open(self.source / self.name, "w:gz") as t:
            t.add(self.tree / "Clawdline Next.app", arcname="Clawdline Next.app")
        manifest = {"version": self.version, "commit": self.sc, "committed_at": "2026-10-22T00:00:00Z",
                    "artifacts": [{"name": self.name, "kind": "app", "os": "darwin", "arch": "arm64",
                                   "sha256": dmg.digest(self.source / self.name), "size": (self.source / self.name).stat().st_size}]}
        dmg.write_json(self.source / "manifest.json", manifest)
        dmg.write_json(self.source / (self.name + ".spdx.json"), {"documentNamespace": "https://example.invalid/app"})
        (self.delivery / self.name.replace(".tar.gz", ".dmg")).write_bytes(b"synthetic image")

    def documents(self, **kwargs):
        return dmg.documents(self.source, self.delivery, self.tree, self.mount, self.version, self.sc, self.pc,
                             ".github/workflows/dmg.yml", **kwargs)

    def test_unchanged_payload_separates_source_from_packaging_and_binds_sbom(self):
        evidence, sbom = self.documents()
        self.assertEqual(evidence["source_commit"], self.sc)
        self.assertEqual(evidence["packaging_commit"], self.pc)
        self.assertFalse(evidence["fixture"])
        self.assertEqual(sbom["externalDocumentRefs"][0]["checksum"]["checksumValue"],
                         dmg.digest(self.source / (self.name + ".spdx.json")))
        self.assertTrue(any(f["fileName"] == "./Install.txt" for f in sbom["files"]))

    def test_mutated_app_fails_even_when_image_digest_is_current(self):
        (self.mount / "Clawdline Next.app/Contents/executable").write_bytes(b"replacement")
        with self.assertRaisesRegex(ValueError, "payload differs"):
            self.documents()

    def test_mode_change_fails(self):
        (self.mount / "Clawdline Next.app/Contents/executable").chmod(0o644)
        with self.assertRaisesRegex(ValueError, "payload differs"):
            self.documents()

    def test_missing_extra_link_and_instructions_fail(self):
        p = self.mount / "Applications"
        p.unlink()
        p.symlink_to("/tmp")
        with self.assertRaisesRegex(ValueError, "Applications must link"):
            self.documents()
        p.unlink()
        p.symlink_to("/Applications")
        (self.mount / "hidden").write_text("unexpected")
        with self.assertRaisesRegex(ValueError, "top-level entries"):
            self.documents()
        (self.mount / "hidden").unlink()
        (self.mount / "Install.txt").write_text("edited")
        with self.assertRaisesRegex(ValueError, "instructions"):
            self.documents()

    def test_changed_source_and_workflow_fail(self):
        manifest = dmg.read_json(self.source / "manifest.json")
        manifest["commit"] = self.pc
        dmg.write_json(self.source / "manifest.json", manifest)
        with self.assertRaisesRegex(ValueError, "commit differs"):
            self.documents()

    def test_fixture_is_distinct_evidence(self):
        self.assertNotEqual(self.documents()[0], self.documents(fixture=True)[0])

    def test_unknown_packaging_workflow_is_refused(self):
        with self.assertRaisesRegex(ValueError, "packaging commit or workflow"):
            dmg.documents(self.source, self.delivery, self.tree, self.mount, self.version, self.sc, self.pc,
                          ".github/workflows/dmg.yml-copy.yml")

    def test_source_archive_substitution_fails(self):
        with open(self.source / self.name, "ab") as f:
            f.write(b"replacement")
        with self.assertRaisesRegex(ValueError, "archive differs"):
            self.documents()

    def test_app_root_cannot_be_an_external_symlink(self):
        app = self.mount / "Clawdline Next.app"
        app.rename(self.mount / "elsewhere")
        app.symlink_to(self.mount / "elsewhere")
        with self.assertRaisesRegex(ValueError, "not a symlink"):
            self.documents()

    def test_cli_rejects_stale_delivery_sbom_and_fixture_claims(self):
        script = Path(__file__).with_name("dmg-evidence.py")
        args = [str(p) for p in (self.source, self.delivery, self.tree, self.mount)] + [self.version, self.sc, self.pc, ".github/workflows/dmg.yml"]
        def run(mode, *flags):
            return subprocess.run([sys.executable, "-B", str(script), mode, *args, *flags], capture_output=True, text=True)
        self.assertEqual(run("generate").returncode, 0)
        self.assertEqual(run("verify").returncode, 0)
        record = self.delivery / (self.name.replace(".tar.gz", ".dmg.delivery.json"))
        doc = dmg.read_json(record)
        doc["fixture"] = True
        dmg.write_json(record, doc)
        result = run("verify")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("does not describe", result.stderr)
        self.assertEqual(run("generate").returncode, 0)
        sbom_path = self.delivery / self.name.replace(".tar.gz", ".dmg.spdx.json")
        sbom = dmg.read_json(sbom_path)
        sbom["files"].pop()
        dmg.write_json(sbom_path, sbom)
        self.assertNotEqual(run("verify").returncode, 0)

    def test_safe_archive_extracts_without_running_it(self):
        target = self.source / "unpacked"
        dmg.extract_app(self.source / self.name, target)
        self.assertEqual(dmg.inventory(target), dmg.inventory(self.tree))

    def test_malicious_archive_paths_and_link_parents_fail(self):
        for name, link in (("../escape", None), ("/escape", None),
                           ("Clawdline Next.app/Contents/escape", "/tmp"),
                           ("Clawdline Next.app/Contents/escape", "../../../escape")):
            with self.subTest(name=name, link=link):
                archive = self.source / "unsafe.tar.gz"
                with tarfile.open(archive, "w:gz") as t:
                    entry = tarfile.TarInfo(name)
                    if link is not None:
                        entry.type, entry.linkname = tarfile.SYMTYPE, link
                        t.addfile(entry)
                    else:
                        entry.size = 1
                        t.addfile(entry, io.BytesIO(b"x"))
                with tempfile.TemporaryDirectory() as root:
                    with self.assertRaisesRegex(ValueError, "unsafe archive|escaping app"):
                        dmg.extract_app(archive, Path(root) / "out")

    def test_duplicate_entries_and_entries_below_symlinks_fail(self):
        for duplicate in (True, False):
            with self.subTest(duplicate=duplicate):
                archive = self.source / "unsafe.tar.gz"
                with tarfile.open(archive, "w:gz") as t:
                    first = tarfile.TarInfo("Clawdline Next.app/link")
                    first.type, first.linkname = tarfile.SYMTYPE, "Contents"
                    t.addfile(first)
                    second = tarfile.TarInfo("Clawdline Next.app/./link" if duplicate else "Clawdline Next.app/link/child")
                    second.size = 1
                    t.addfile(second, io.BytesIO(b"x"))
                with tempfile.TemporaryDirectory() as root:
                    with self.assertRaisesRegex(ValueError, "unsafe archive|below symlink"):
                        dmg.extract_app(archive, Path(root) / "out")


if __name__ == "__main__":
    unittest.main(verbosity=2)
