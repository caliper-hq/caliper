import { parse, stringify } from 'yaml';

export type EvaluatorRule = { id: string; type: 'regex' | 'semantic' | 'regression'; pattern: string };

export type Suite = {
  filePath?: string;
  datasetId: string;
  datasetName: string;
  prompt: string;
  expected: string;
  evaluators: EvaluatorRule[];
};

export function serializeSuite(suite: Suite): string {
  const isModularFile = suite.filePath && suite.filePath.startsWith('datasets/');
  if (isModularFile) {
    return stringify({
      datasets: [
        {
          id: suite.datasetId,
          name: suite.datasetName,
          profile: 'mock-geography',
          test_cases: [
            {
              id: 'dashboard-case',
              prompt: suite.prompt,
              expected: suite.expected || undefined,
            },
          ],
          evaluators: suite.evaluators.map((rule) => ({
            id: rule.id,
            type: rule.type,
            depends_on: [],
            params: rule.type === 'regex' ? { pattern: rule.pattern } : { rubric: rule.pattern },
          })),
        },
      ],
    });
  }

  return stringify({
    version: '1.0',
    profiles: [{ name: 'dashboard-mock', provider: { type: 'mock', params: { response: '' } } }],
    datasets: [
      {
        id: suite.datasetId,
        name: suite.datasetName,
        profile: 'dashboard-mock',
        test_cases: [
          {
            id: 'dashboard-case',
            prompt: suite.prompt,
            expected: suite.expected || undefined,
          },
        ],
        evaluators: suite.evaluators.map((rule) => ({
          id: rule.id,
          type: rule.type,
          depends_on: [],
          params: rule.type === 'regex' ? { pattern: rule.pattern } : { rubric: rule.pattern },
        })),
      },
    ],
    reporters: [{ type: 'console' }],
  });
}

export function parseSuiteYaml(yamlText: string): Suite | null {
  try {
    const doc = parse(yamlText);
    if (!doc) return null;
    const datasets = doc.datasets || [];
    if (datasets.length === 0) return null;
    const ds = datasets[0];
    const tc = ds.test_cases?.[0] || {};
    const evals = (ds.evaluators || []).map((e: any) => ({
      id: e.id || 'rule',
      type: e.type || 'regex',
      pattern: e.params?.pattern || e.params?.rubric || '',
    }));
    return {
      datasetId: ds.id || 'new-suite',
      datasetName: ds.name || 'Dataset Suite',
      prompt: tc.prompt || '',
      expected: tc.expected || '',
      evaluators: evals.length > 0 ? evals : [{ id: 'rule-1', type: 'regex', pattern: '.*' }],
    };
  } catch (e) {
    return null;
  }
}
