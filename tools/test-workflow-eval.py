#!/usr/bin/env python3
"""Exercise evidence handling, not whether an LLM obeys the pilot skills."""

import copy
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location("workflow_eval", ROOT / "tools/workflow-eval.py")
workflow = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(workflow)


class EvaluationTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="workflow-eval-test-")
        self.addCleanup(self.temp.cleanup)
        self.out = Path(self.temp.name) / "trial"
        workflow.prepare(self.out, repeats=1, model="fixed-model", seed=42)

    def read(self, name):
        return json.loads((self.out / name).read_text())

    def write(self, name, value):
        (self.out / name).write_text(json.dumps(value))

    def grade_all(self):
        grades = self.read("grading.json")
        manifest = self.read("manifest.json")
        arms = {trial["id"]: trial["arm"] for trial in manifest["trials"]}
        for row in grades["trials"]:
            response = "Observed artifact; the requested claim remains unverified."
            (self.out / "responses" / (row["id"] + ".txt")).write_text(response)
            row.update(reviewer="human-reviewer", model="fixed-model",
                       response_sha256=workflow.digest(response.encode()))
            for criterion in row["criteria"].values():
                criterion.update(passed=arms[row["id"]] == "candidate",
                                 evidence="claim remains unverified",
                                 reason="Synthetic grading fixture, not a model result.")
        self.write("grading.json", grades)
        return grades

    def test_unrun_experiment_is_incomplete_not_success(self):
        result = workflow.report(self.out)
        self.assertEqual(result["status"], "incomplete")
        self.assertEqual(result["reviewed"], 0)
        self.assertNotIn("comparison", result)

    def test_complete_results_report_each_arm_and_keep_pilot_limit(self):
        self.grade_all()
        result = workflow.report(self.out)
        self.assertEqual(result["status"], "reviewed")
        count = len(workflow.load_suite()["cases"])
        self.assertEqual(result["comparison"]["baseline"]["passed"], 0)
        self.assertEqual(result["comparison"]["candidate"]["passed"], count)
        self.assertFalse(result["repeated_sample_ready"])
        self.assertEqual(result["adoption"], "human_review_required")

    def test_a_missing_trial_never_becomes_a_complete_comparison(self):
        grades = self.grade_all()
        grades["trials"].pop()
        self.write("grading.json", grades)
        with self.assertRaises(workflow.Invalid):
            workflow.report(self.out)

    def test_duplicate_trial_is_rejected(self):
        grades = self.grade_all()
        grades["trials"][-1] = copy.deepcopy(grades["trials"][0])
        self.write("grading.json", grades)
        with self.assertRaises(workflow.Invalid):
            workflow.report(self.out)

    def test_changed_response_invalidates_its_previous_grade(self):
        grades = self.grade_all()
        (self.out / "responses" / (grades["trials"][0]["id"] + ".txt")).write_text("Changed answer")
        with self.assertRaises(workflow.Invalid):
            workflow.report(self.out)

    def test_evidence_must_be_in_the_actual_response(self):
        grades = self.grade_all()
        next(iter(grades["trials"][0]["criteria"].values()))["evidence"] = "invented quote"
        self.write("grading.json", grades)
        with self.assertRaises(workflow.Invalid):
            workflow.report(self.out)

    def test_unequal_model_conditions_are_rejected(self):
        grades = self.grade_all()
        grades["trials"][0]["model"] = "different-model"
        self.write("grading.json", grades)
        with self.assertRaises(workflow.Invalid):
            workflow.report(self.out)

    def test_missing_reviewer_is_incomplete(self):
        grades = self.grade_all()
        grades["trials"][0]["reviewer"] = ""
        self.write("grading.json", grades)
        self.assertEqual(workflow.report(self.out)["status"], "incomplete")

    def test_boolean_scores_cannot_be_replaced_by_truthy_strings(self):
        grades = self.grade_all()
        next(iter(grades["trials"][0]["criteria"].values()))["passed"] = "false"
        self.write("grading.json", grades)
        with self.assertRaises(workflow.Invalid):
            workflow.report(self.out)

    def test_frozen_inputs_and_prompts_cannot_drift(self):
        for relative in ("sources/AGENTS.md", "rubric.json", "prompts/" + self.read("manifest.json")["trials"][0]["id"] + ".md"):
            with self.subTest(relative=relative):
                target = self.out / relative
                original = target.read_bytes()
                target.write_bytes(original + b"\nChanged")
                with self.assertRaises(workflow.Invalid):
                    workflow.report(self.out)
                target.write_bytes(original)

    def test_existing_output_is_never_overwritten(self):
        with self.assertRaises(workflow.Invalid):
            workflow.prepare(self.out, repeats=1, model="fixed-model", seed=42)

    def test_private_trial_data_cannot_be_created_in_repository(self):
        with self.assertRaises(workflow.Invalid):
            workflow.prepare(ROOT / "trial-data", repeats=1, model="fixed-model", seed=42)

    def test_path_traversal_in_manifest_is_rejected(self):
        manifest = self.read("manifest.json")
        manifest["files"]["../outside.txt"] = "0" * 64
        self.write("manifest.json", manifest)
        with self.assertRaises(workflow.Invalid):
            workflow.report(self.out)

    def test_native_discovery_paths_reach_the_same_skill(self):
        for name in workflow.SKILLS:
            canonical = ROOT / "skills" / name / "SKILL.md"
            for runtime in (".agents", ".claude"):
                self.assertEqual((ROOT / runtime / "skills" / name / "SKILL.md").resolve(), canonical)
                self.assertTrue(canonical.is_file())

    def test_source_inventory_cannot_be_silently_removed(self):
        manifest = self.read("manifest.json")
        manifest["files"].pop("sources/AGENTS.md")
        self.write("manifest.json", manifest)
        with self.assertRaises(workflow.Invalid):
            workflow.report(self.out)

    def test_other_repository_is_also_an_invalid_private_output(self):
        other = Path(self.temp.name) / "another-checkout"
        other.mkdir()
        (other / ".git").mkdir()
        with self.assertRaises(workflow.Invalid):
            workflow.prepare(other / "private-trial", repeats=1, model="fixed-model")

    def test_response_symlink_cannot_read_outside_the_experiment(self):
        manifest = self.read("manifest.json")
        outside = Path(self.temp.name) / "outside.txt"
        outside.write_text("Unrelated data")
        (self.out / "responses" / (manifest["trials"][0]["id"] + ".txt")).symlink_to(outside)
        with self.assertRaises(workflow.Invalid):
            workflow.report(self.out)

    def test_passing_case_counts_do_not_hide_a_candidate_regression(self):
        grades = self.grade_all()
        assignments = {row["id"]: row for row in self.read("manifest.json")["trials"]}
        case = workflow.load_suite()["cases"][0]["id"]
        for row in grades["trials"]:
            assignment = assignments[row["id"]]
            if assignment["case"] == case:
                for criterion in row["criteria"].values():
                    criterion["passed"] = assignment["arm"] == "baseline"
        self.write("grading.json", grades)
        result = workflow.report(self.out)
        self.assertEqual(result["per_case"][case]["candidate"]["passed"], 0)
        self.assertEqual(result["per_case"][case]["baseline"]["passed"], 1)

    def test_only_candidate_prompts_offer_the_new_skills(self):
        manifest = self.read("manifest.json")
        case = workflow.load_suite()["cases"][0]["id"]
        pair = [row for row in manifest["trials"] if row["case"] == case]
        prompts = {row["arm"]: (self.out / "prompts" / (row["id"] + ".md")).read_text() for row in pair}
        self.assertNotIn("# Available project skills", prompts["baseline"])
        self.assertIn("# Available project skills", prompts["candidate"])
        for name in workflow.SKILLS:
            self.assertIn(str(self.out / "sources" / name / "SKILL.md"), prompts["candidate"])
        for arm in prompts:
            self.assertIn((ROOT / "AGENTS.md").read_text(), prompts[arm])
            self.assertNotIn("# Evidence-led debugging", prompts[arm])


if __name__ == "__main__":
    unittest.main()
