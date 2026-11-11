import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location('paired_analysis', Path(__file__).with_name('analyze_paired_models.py'))
analysis = importlib.util.module_from_spec(spec)
spec.loader.exec_module(analysis)


class PairedAnalysisTest(unittest.TestCase):
    def test_constant_cluster_ratio(self):
        interval = analysis.clustered_interval([('a', 2), ('a', 2), ('b', 2)], iterations=100)
        self.assertEqual(interval['estimate'], 2)
        self.assertEqual(interval['ci95'], [2, 2])
        self.assertEqual(interval['scenarioFamilies'], 2)

    def test_failures_remain_and_reversed_blind_labels_map_to_correct_model(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            def save(name, value):
                p = root / name
                p.parent.mkdir(parents=True, exist_ok=True)
                p.write_text(json.dumps(value))
            cases = [{'ID': 'writer', 'Family': 'writer', 'Writer': True},
                     {'ID': 'state', 'Family': 'state', 'Writer': False}]
            schedule = [{'ID': 'writer-r1', 'CaseID': 'writer'}, {'ID': 'state-r1', 'CaseID': 'state'}]
            save('plan.json', {'Cases': cases, 'Schedule': schedule, 'Notes': [], 'Version': 'test',
                              'PromptRevision': 25, 'ConfigRevision': 28, 'CallDeadlineSeconds': 180})
            save('state.json', {'status': 'complete'})
            weights = dict(continuity=.30, agency=.25, sceneProgress=.20, prose=.15, nonRepetition=.10)
            save('analysis-spec.json', {'writerWeights': weights, 'bootstrapIterations': 100, 'bootstrapSeed': 1})
            def outcome(ok, elapsed):
                return {'OK': ok, 'ElapsedMS': elapsed, 'InputSHA256': 'same', 'HTTPPosts': 1,
                        'ErrorCode': '' if ok else 'empty', 'Checks': dict(schemaScore=1, instructionScore=1, languageScore=.95, continuityScore=1)}
            save('pairs/writer-r1.json', {'Spec': schedule[0], 'Outcomes': {'chatgpt': outcome(True, 1000), 'antigravity': outcome(True, 2000)}})
            save('pairs/state-r1.json', {'Spec': schedule[1], 'Outcomes': {'chatgpt': outcome(False, 1000), 'antigravity': outcome(True, 2000)}})
            for judge, a_arm in [('chatgpt', 'chatgpt'), ('antigravity', 'antigravity')]:
                scores = {'A': {k: 4 if a_arm == 'chatgpt' else 2 for k in weights},
                          'B': {k: 2 if a_arm == 'chatgpt' else 4 for k in weights},
                          'winner': 'A' if a_arm == 'chatgpt' else 'B', 'criticalErrorsA': [], 'criticalErrorsB': []}
                save('judgments/writer-r1-' + judge + '.json', {'judge': judge, 'pair': 'writer-r1',
                     'validVerdict': True, 'aArm': a_arm, 'bArm': 'antigravity' if a_arm == 'chatgpt' else 'chatgpt',
                     'outcome': {'Output': scores}})
            save('bridge-before.json', {'jobs': [{'id': 'old', 'state': 'failed'}]})
            save('bridge-roles.json', {'jobs': [{'id': 'old', 'state': 'failed'}, {'id': 'new', 'state': 'complete'}]})
            result = analysis.analyze(root)
            self.assertEqual(result['bridgeJobStatesByPhase']['roles'], {'complete': 1})
            self.assertEqual(result['arms']['all']['chatgpt']['validJSONObjects'], 1)
            self.assertEqual(result['arms']['all']['recorded'], 2)
            self.assertEqual(result['arms']['structured']['antigravity']['schemaInstructionContinuityPass'], 1)
            self.assertEqual(result['arms']['structured']['antigravity']['strictAllScoresPerfect'], 0)
            self.assertLess(result['latency']['all']['bothValidOnly']['ratio'], 1)
            self.assertGreater(result['latency']['all']['failurePenaltySensitivity']['ratio'], 1)
            self.assertEqual(result['quality']['primaryPairsWithTwoValidJudges'], 1)
            self.assertAlmostEqual(result['quality']['primaryWeightedDeltaCGminusAG']['estimate'], 2)
            self.assertEqual(result['quality']['primaryWeightedWinners'], {'chatgpt': 1})


if __name__ == '__main__':
    unittest.main()
