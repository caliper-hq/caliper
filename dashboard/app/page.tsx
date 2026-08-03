'use client';

import { useEffect, useMemo, useState, ChangeEvent } from 'react';
import { EvaluatorRule, parseSuiteYaml, serializeSuite } from '../lib/serialize';

const initialRules: EvaluatorRule[] = [{ id: 'response-check', type: 'regex', pattern: '.*' }];

type RunRecord = {
  run_id: string;
  dataset_id: string;
  timestamp: string;
  config_version: string;
  source: 'postgres' | 'local';
  fileName?: string;
  telemetry: {
    avg_latency_ms: number;
    cost_usd: number;
    overall_score: number;
    passed: boolean;
  };
  results?: any[];
};

type WorkspaceFile = {
  relativePath: string;
  name: string;
  content: string;
};

export default function Home() {
  // Navigation / Tabs
  const [activeTab, setActiveTab] = useState<'editor' | 'runs'>('editor');

  // User Auth & Credentials State
  const [authMode, setAuthMode] = useState<'login' | 'register' | 'forgot'>('login');
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [resetTokenInput, setResetTokenInput] = useState('');
  const [newPasswordInput, setNewPasswordInput] = useState('');
  const [userId, setUserId] = useState<string | null>(null);
  const [loggedInUser, setLoggedInUser] = useState<string | null>(null);
  const [project, setProject] = useState('default');
  const [token, setToken] = useState('');
  const [repository, setRepository] = useState('owner/repository');
  const [authStatus, setAuthStatus] = useState('');
  const [showAuthModal, setShowAuthModal] = useState(false);

  // Dataset Files & Tree State
  const [existingFiles, setExistingFiles] = useState<string[]>([
    'datasets/geography-qa.yml',
    'datasets/code-generation.yml',
    'caliper.yml',
  ]);
  const [folderFiles, setFolderFiles] = useState<WorkspaceFile[]>([]);
  const [folderName, setFolderName] = useState<string>('');
  const [selectedFile, setSelectedFile] = useState<string>('');

  // Suite Editor Form State
  const [datasetId, setDatasetId] = useState('geography-qa');
  const [datasetName, setDatasetName] = useState('Geography Q&A Suite');
  const [prompt, setPrompt] = useState('What is the capital of France?');
  const [expected, setExpected] = useState('Paris');
  const [rules, setRules] = useState<EvaluatorRule[]>(initialRules);
  const [saveStatus, setSaveStatus] = useState('');

  // Runs Dashboard State
  const [dataSource, setDataSource] = useState<'postgres' | 'local'>('postgres');
  const [runsList, setRunsList] = useState<RunRecord[]>([]);
  const [loadingRuns, setLoadingRuns] = useState(false);
  const [selectedRunJson, setSelectedRunJson] = useState<any | null>(null);

  const apiBase = process.env.NEXT_PUBLIC_CONTROL_PLANE_URL ?? 'http://localhost:3000';

  // Load auth state from LocalStorage on mount
  useEffect(() => {
    const savedUser = localStorage.getItem('caliper_user');
    const savedToken = localStorage.getItem('caliper_token');
    const savedRepo = localStorage.getItem('caliper_repo');
    const savedProject = localStorage.getItem('caliper_project');
    const savedUserId = localStorage.getItem('caliper_user_id');

    if (savedUser) {
      setLoggedInUser(savedUser);
    } else {
      setShowAuthModal(true);
    }
    if (savedToken) setToken(savedToken);
    if (savedRepo) setRepository(savedRepo);
    if (savedProject) setProject(savedProject || 'default');
    if (savedUserId) setUserId(savedUserId);

    fetchExistingFiles();
  }, []);

  // Fetch list of dataset files from backend API
  async function fetchExistingFiles() {
    try {
      const res = await fetch(`${apiBase}/v1/git-bridge/files`);
      if (res.ok) {
        const files = await res.json();
        if (files && files.length > 0) {
          setExistingFiles(files);
        }
      }
    } catch (err) {
      console.error('Failed to fetch existing dataset files', err);
    }
  }

  // Load dataset file content when user selects a file from dropdown or tree
  async function handleFileSelect(filePath: string) {
    setSelectedFile(filePath);
    if (!filePath) return;

    // Check if file is in loaded folder workspace first
    const matchFolderFile = folderFiles.find((f) => f.relativePath === filePath || f.name === filePath);
    if (matchFolderFile) {
      const parsed = parseSuiteYaml(matchFolderFile.content);
      if (parsed) {
        setDatasetId(parsed.datasetId);
        setDatasetName(parsed.datasetName);
        setPrompt(parsed.prompt);
        setExpected(parsed.expected);
        setRules(parsed.evaluators);
        setSaveStatus(`Loaded content from workspace file: ${matchFolderFile.relativePath}`);
        return;
      }
    }

    try {
      const res = await fetch(`${apiBase}/v1/git-bridge/file-content?path=${encodeURIComponent(filePath)}`);
      if (res.ok) {
        const text = await res.text();
        const parsed = parseSuiteYaml(text);
        if (parsed) {
          setDatasetId(parsed.datasetId);
          setDatasetName(parsed.datasetName);
          setPrompt(parsed.prompt);
          setExpected(parsed.expected);
          setRules(parsed.evaluators);
          setSaveStatus(`Loaded content from ${filePath}`);
        }
      }
    } catch (err) {
      setSaveStatus(`Could not read file: ${(err as Error).message}`);
    }
  }

  // Pick & Load a Single Local YAML File
  function handleLocalFileUpload(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = (event) => {
      const text = event.target?.result as string;
      if (text) {
        const parsed = parseSuiteYaml(text);
        if (parsed) {
          setSelectedFile(file.name);
          setDatasetId(parsed.datasetId);
          setDatasetName(parsed.datasetName);
          setPrompt(parsed.prompt);
          setExpected(parsed.expected);
          setRules(parsed.evaluators);
          setSaveStatus(`Loaded local file: ${file.name}`);
        } else {
          setSaveStatus(`Failed to parse YAML structure from ${file.name}`);
        }
      }
    };
    reader.readAsText(file);
  }

  // Pick & Load an Entire Workspace Folder (Directory Tree)
  function handleFolderSelect(e: ChangeEvent<HTMLInputElement>) {
    const files = e.target.files;
    if (!files || files.length === 0) return;

    const parsedFiles: WorkspaceFile[] = [];
    let rootDirName = '';
    let readCount = 0;

    for (let i = 0; i < files.length; i++) {
      const file = files[i];
      const relPath = file.webkitRelativePath || file.name;
      if (!rootDirName && relPath.includes('/')) {
        rootDirName = relPath.split('/')[0];
      }

      if (file.name.endsWith('.yml') || file.name.endsWith('.yaml')) {
        const reader = new FileReader();
        reader.onload = (event) => {
          const content = (event.target?.result as string) || '';
          parsedFiles.push({
            relativePath: relPath,
            name: file.name,
            content,
          });
          readCount++;

          if (readCount === files.length) {
            setFolderFiles(parsedFiles);
            setFolderName(rootDirName || 'Loaded Folder');
            setSaveStatus(`Loaded ${parsedFiles.length} dataset YAML file(s) from folder "${rootDirName}"`);
            if (parsedFiles.length > 0) {
              handleFileSelect(parsedFiles[0].relativePath);
            }
          }
        };
        reader.readAsText(file);
      } else {
        readCount++;
      }
    }
  }

  // Pick & Load Local Run JSON File(s) / Folder from user's machine (.caliper/history)
  function handleLocalRunFileUpload(e: ChangeEvent<HTMLInputElement>) {
    const files = e.target.files;
    if (!files || files.length === 0) return;
    const newRuns: RunRecord[] = [];
    let readCount = 0;
    for (let i = 0; i < files.length; i++) {
      const file = files[i];
      if (file.name.endsWith('.json') && !file.name.endsWith('.tmp')) {
        const reader = new FileReader();
        reader.onload = (event) => {
          try {
            const text = event.target?.result as string;
            if (text) {
              const data = JSON.parse(text);
              if (data.run_id) {
                data.source = 'local';
                data.fileName = file.name;
                newRuns.push(data);
              }
            }
          } catch (err) {
            console.error('Failed to parse run JSON', err);
          }
          readCount++;
          if (readCount === files.length) {
            newRuns.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
            setRunsList((prev) => [...newRuns, ...prev]);
            setDataSource('local');
          }
        };
        reader.readAsText(file);
      } else {
        readCount++;
      }
    }
  }

  // User Auth: Register / Login
  async function handleLogin(isRegister = false) {
    setAuthStatus('Authenticating…');
    try {
      const endpoint = isRegister ? `${apiBase}/v1/auth/register` : `${apiBase}/v1/auth/login`;
      const payload = isRegister
        ? { username, password, githubToken: token, repository, projectId: project }
        : { username, password };
      const res = await fetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      const data = await res.json();
      if (!res.ok) {
        const msg = Array.isArray(data.message) ? data.message.join(', ') : (data.message || 'Auth failed');
        throw new Error(msg);
      }

      setLoggedInUser(data.username);
      setUserId(data.id);
      if (data.githubToken) setToken(data.githubToken);
      if (data.repository) setRepository(data.repository);
      if (data.projectId) setProject(data.projectId);

      localStorage.setItem('caliper_user', data.username);
      localStorage.setItem('caliper_user_id', data.id);
      if (data.githubToken) localStorage.setItem('caliper_token', data.githubToken);
      localStorage.setItem('caliper_repo', data.repository || repository);
      localStorage.setItem('caliper_project', data.projectId || project);

      setAuthStatus('Authenticated successfully!');
      setTimeout(() => setShowAuthModal(false), 800);
    } catch (err) {
      setAuthStatus((err as Error).message);
    }
  }

  // Generate Reset Token for Password
  async function handleRequestReset() {
    setAuthStatus('Generating reset token…');
    try {
      const res = await fetch(`${apiBase}/v1/auth/forgot-password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username }),
      });
      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.message || 'Failed to generate reset link');
      }
      setResetTokenInput(data.resetToken);
      setAuthStatus(`Reset token generated for @${username}! Token code: ${data.resetToken}`);
    } catch (err) {
      setAuthStatus((err as Error).message);
    }
  }

  // Confirm Password Reset with Token
  async function handleResetPassword() {
    setAuthStatus('Updating password in database…');
    try {
      const res = await fetch(`${apiBase}/v1/auth/reset-password`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, resetToken: resetTokenInput, newPassword: newPasswordInput }),
      });
      const data = await res.json();
      if (!res.ok) {
        throw new Error(data.message || 'Password reset failed');
      }
      setAuthStatus('Password reset successfully! You can now log in.');
      setPassword(newPasswordInput);
      setAuthMode('login');
    } catch (err) {
      setAuthStatus((err as Error).message);
    }
  }

  // Save updated user keys to Postgres DB
  async function handleSaveKeys() {
    if (!userId) return;
    setAuthStatus('Updating credentials in Postgres…');
    try {
      const res = await fetch(`${apiBase}/v1/auth/keys/${userId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ githubToken: token, repository, projectId: project }),
      });
      if (res.ok) {
        localStorage.setItem('caliper_token', token);
        localStorage.setItem('caliper_repo', repository);
        localStorage.setItem('caliper_project', project);
        setAuthStatus('Keys saved in Postgres database!');
      } else {
        const data = await res.json();
        const msg = Array.isArray(data.message) ? data.message.join(', ') : (data.message || 'Failed to save keys');
        throw new Error(msg);
      }
    } catch (err) {
      setAuthStatus((err as Error).message);
    }
  }

  // Fetch execution runs from Postgres or Local API
  async function fetchRuns() {
    setLoadingRuns(true);
    try {
      if (dataSource === 'postgres') {
        let res = await fetch(`${apiBase}/v1/projects/${project}/runs/list`);
        let data: RunRecord[] = res.ok ? await res.json() : [];
        if (!data || data.length === 0) {
          res = await fetch(`${apiBase}/v1/projects/default/runs/list`);
          const defaultData = res.ok ? await res.json() : [];
          if (defaultData && defaultData.length > 0) data = defaultData;
        }
        if (!data || data.length === 0) {
          res = await fetch(`${apiBase}/v1/projects/team-a/runs/list`);
          const teamAData = res.ok ? await res.json() : [];
          if (teamAData && teamAData.length > 0) data = teamAData;
        }
        setRunsList(data || []);
      } else {
        const res = await fetch(`${apiBase}/v1/projects/${project}/runs/local`);
        if (res.ok) {
          const data = await res.json();
          setRunsList(data || []);
        }
      }
    } catch (err) {
      console.error('Failed to fetch runs:', err);
    } finally {
      setLoadingRuns(false);
    }
  }

  useEffect(() => {
    if (activeTab === 'runs') {
      fetchRuns();
    }
  }, [activeTab, dataSource, project]);

  const generatedYaml = useMemo(
    () =>
      serializeSuite({
        filePath: selectedFile,
        datasetId,
        datasetName,
        prompt,
        expected,
        evaluators: rules,
      }),
    [selectedFile, datasetId, datasetName, prompt, expected, rules],
  );

  const changeRule = (index: number, change: Partial<EvaluatorRule>) =>
    setRules((current) => current.map((rule, i) => (i === index ? { ...rule, ...change } : rule)));

  async function saveSuiteAsPR() {
    setSaveStatus('Opening GitHub pull request…');
    const targetFile = selectedFile || `datasets/${datasetId.toLowerCase().replace(/[^a-z0-9-]/g, '-')}.yml`;
    const branch = `suite-${datasetId.toLowerCase().replace(/[^a-z0-9-]/g, '-')}-${Date.now()}`;
    try {
      const response = await fetch(`${apiBase}/v1/git-bridge/pull-request`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${token}`,
          'x-user-github-token': token,
        },
        body: JSON.stringify({
          project_id: project,
          repository,
          yaml: generatedYaml,
          file_path: targetFile,
          title: `chore(caliper): update ${datasetName}`,
          branch_name: branch,
        }),
      });
      const body = (await response.json()) as { url?: string; message?: string };
      if (!response.ok) throw new Error(body.message ?? 'Failed to create pull request');
      setSaveStatus(`Pull Request created: ${body.url}`);
    } catch (error) {
      setSaveStatus(`Could not create pull request: ${(error as Error).message}`);
    }
  }

  // Analytics Metrics Computed from Active Runs List
  const analytics = useMemo(() => {
    const total = runsList.length;
    if (total === 0) return { total: 0, passed: 0, failed: 0, passRate: 0, avgScore: 0, avgLatency: 0, totalCost: 0 };
    const passed = runsList.filter((r) => r.telemetry?.passed).length;
    const failed = total - passed;
    const passRate = Math.round((passed / total) * 100);
    const sumScore = runsList.reduce((acc, r) => acc + (r.telemetry?.overall_score ?? 0), 0);
    const sumLatency = runsList.reduce((acc, r) => acc + (r.telemetry?.avg_latency_ms ?? 0), 0);
    const sumCost = runsList.reduce((acc, r) => acc + (r.telemetry?.cost_usd ?? 0), 0);
    return {
      total,
      passed,
      failed,
      passRate,
      avgScore: Number((sumScore / total).toFixed(2)),
      avgLatency: Math.round(sumLatency / total),
      totalCost: Number(sumCost.toFixed(4)),
    };
  }, [runsList]);

  return (
    <main className="min-h-screen bg-slate-950 text-slate-100 font-sans selection:bg-cyan-500 selection:text-slate-950">
      {/* Top Header & Navbar */}
      <header className="border-b border-slate-800 bg-slate-900/90 backdrop-blur sticky top-0 z-40 px-8 py-4">
        <div className="mx-auto max-w-7xl flex flex-wrap items-center justify-between gap-4">
          <div className="flex items-center space-x-3">
            <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-cyan-400 font-black text-slate-950 text-base shadow-lg shadow-cyan-500/20">
              C
            </span>
            <div>
              <h1 className="text-xl font-black tracking-wider text-white">CALIPER</h1>
              <p className="text-xs font-medium text-slate-400">Enterprise AI Evaluation Platform</p>
            </div>
          </div>

          {/* Navigation Tabs */}
          <div className="flex rounded-xl bg-slate-950 p-1.5 border border-slate-800">
            <button
              onClick={() => setActiveTab('editor')}
              className={`px-5 py-2 text-xs font-bold rounded-lg transition-all ${
                activeTab === 'editor'
                  ? 'bg-cyan-400 text-slate-950 shadow-md shadow-cyan-500/10'
                  : 'text-slate-300 hover:text-white hover:bg-slate-900'
              }`}
            >
              Suite Editor & Datasets
            </button>
            <button
              onClick={() => setActiveTab('runs')}
              className={`px-5 py-2 text-xs font-bold rounded-lg transition-all ${
                activeTab === 'runs'
                  ? 'bg-cyan-400 text-slate-950 shadow-md shadow-cyan-500/10'
                  : 'text-slate-300 hover:text-white hover:bg-slate-900'
              }`}
            >
              Execution Runs Dashboard
            </button>
          </div>

          {/* User Auth Status Button */}
          <div className="flex items-center space-x-3">
            {loggedInUser ? (
              <div className="flex items-center space-x-2">
                <span className="inline-flex items-center rounded-lg bg-slate-800 px-3 py-1.5 text-xs font-semibold text-cyan-300 border border-slate-700">
                  User: @{loggedInUser}
                </span>
                <button
                  onClick={() => setShowAuthModal(true)}
                  className="rounded-lg bg-slate-800 px-3 py-1.5 text-xs font-semibold text-slate-200 hover:bg-slate-700 border border-slate-700 transition"
                >
                  Manage Keys
                </button>
              </div>
            ) : (
              <button
                onClick={() => setShowAuthModal(true)}
                className="rounded-lg bg-cyan-400 px-5 py-2 text-xs font-black text-slate-950 hover:bg-cyan-300 shadow-md shadow-cyan-500/20 transition"
              >
                Log In / Register
              </button>
            )}
          </div>
        </div>
      </header>

      <div className="mx-auto max-w-7xl p-8 space-y-8">
        {/* TAB 1: SUITE EDITOR & DATASETS */}
        {activeTab === 'editor' && (
          <div className="space-y-6">
            {/* Existing & Local Dataset Selector Bar */}
            <div className="flex flex-wrap items-center justify-between rounded-2xl bg-slate-900/90 p-5 border border-slate-800 shadow-lg gap-4">
              <div className="flex flex-wrap items-center gap-3">
                <span className="text-xs font-bold uppercase tracking-wider text-cyan-400">Load Dataset</span>
                
                {/* Repo Dropdown */}
                <select
                  value={selectedFile}
                  onChange={(e) => handleFileSelect(e.target.value)}
                  className="rounded-xl bg-slate-950 px-4 py-2.5 text-xs font-medium text-slate-100 border border-slate-700 focus:border-cyan-400 focus:outline-none min-w-[240px]"
                >
                  <option value="">-- Select Repo File --</option>
                  {existingFiles.map((file) => (
                    <option key={file} value={file}>
                      📄 {file}
                    </option>
                  ))}
                </select>

                {/* Single File Picker */}
                <label className="cursor-pointer rounded-xl bg-slate-800 px-3.5 py-2.5 text-xs font-bold text-slate-200 hover:bg-slate-700 border border-slate-700 transition flex items-center space-x-1.5 shadow">
                  <span>📄 Open Local File</span>
                  <input
                    type="file"
                    accept=".yml,.yaml"
                    onChange={handleLocalFileUpload}
                    className="hidden"
                  />
                </label>

                {/* Folder Picker */}
                <label className="cursor-pointer rounded-xl bg-cyan-950 px-4 py-2.5 text-xs font-bold text-cyan-300 hover:bg-cyan-900 border border-cyan-800 transition flex items-center space-x-1.5 shadow">
                  <span>📁 Open Dataset Folder</span>
                  <input
                    type="file"
                    // @ts-ignore
                    webkitdirectory=""
                    directory=""
                    multiple
                    onChange={handleFolderSelect}
                    className="hidden"
                  />
                </label>
              </div>

              <button
                onClick={fetchExistingFiles}
                className="rounded-lg bg-slate-800 px-4 py-2 text-xs font-semibold text-cyan-300 hover:bg-slate-700 border border-slate-700 transition"
              >
                🔄 Refresh Repo Files
              </button>
            </div>

            <div className="grid gap-8 lg:grid-cols-[280px_1fr_1fr]">
              {/* Folder Workspace Tree View (Left Sidebar if Folder Loaded) */}
              {folderFiles.length > 0 ? (
                <aside className="rounded-2xl bg-slate-900/90 p-5 border border-slate-800 shadow-lg space-y-3">
                  <div className="flex items-center space-x-2 border-b border-slate-800 pb-3">
                    <span className="text-sm">📁</span>
                    <h3 className="text-xs font-bold uppercase tracking-wider text-cyan-400 truncate">
                      {folderName || 'Workspace Folder'}
                    </h3>
                  </div>
                  <div className="space-y-1 font-mono text-xs max-h-[500px] overflow-auto">
                    {folderFiles.map((file) => (
                      <button
                        key={file.relativePath}
                        onClick={() => handleFileSelect(file.relativePath)}
                        className={`w-full text-left px-3 py-2 rounded-lg transition truncate flex items-center space-x-2 ${
                          selectedFile === file.relativePath
                            ? 'bg-cyan-400 text-slate-950 font-bold shadow'
                            : 'text-slate-300 hover:bg-slate-800'
                        }`}
                      >
                        <span>📄</span>
                        <span className="truncate">{file.relativePath}</span>
                      </button>
                    ))}
                  </div>
                </aside>
              ) : (
                <aside className="rounded-2xl bg-slate-900/50 p-5 border border-slate-800/60 shadow text-center space-y-3 flex flex-col justify-center items-center text-slate-400">
                  <span className="text-3xl">📁</span>
                  <p className="text-xs font-medium">No folder selected yet.</p>
                  <p className="text-[11px] text-slate-500">
                    Click "Open Dataset Folder" to view a tree of all YAML dataset files in a folder.
                  </p>
                </aside>
              )}

              {/* Middle Column: Form Controls */}
              <section className="space-y-5 rounded-2xl bg-slate-900/90 p-6 border border-slate-800 shadow-lg">
                <h2 className="text-lg font-bold text-white tracking-wide">Dataset & Test Cases</h2>

                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1.5">Dataset ID</label>
                  <input
                    value={datasetId}
                    onChange={(e) => setDatasetId(e.target.value)}
                    className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 focus:border-cyan-400 focus:outline-none font-mono"
                  />
                </div>

                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1.5">Dataset Name</label>
                  <input
                    value={datasetName}
                    onChange={(e) => setDatasetName(e.target.value)}
                    className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 focus:border-cyan-400 focus:outline-none"
                  />
                </div>

                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1.5">Test Case Prompt</label>
                  <textarea
                    rows={4}
                    value={prompt}
                    onChange={(e) => setPrompt(e.target.value)}
                    className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 focus:border-cyan-400 focus:outline-none font-mono"
                  />
                </div>

                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1.5">Expected Response (Optional)</label>
                  <input
                    value={expected}
                    onChange={(e) => setExpected(e.target.value)}
                    className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 focus:border-cyan-400 focus:outline-none"
                  />
                </div>

                {/* Evaluator Rules */}
                <div className="pt-2">
                  <div className="mb-3 flex items-center justify-between">
                    <h3 className="text-xs font-bold uppercase tracking-wider text-slate-300">Evaluator Rules (DAG Nodes)</h3>
                    <button
                      onClick={() =>
                        setRules((r) => [...r, { id: `rule-${r.length + 1}`, type: 'regex', pattern: '' }])
                      }
                      className="rounded-lg bg-cyan-950 px-3.5 py-1.5 text-xs font-bold text-cyan-300 hover:bg-cyan-900 border border-cyan-800 transition"
                    >
                      + Add Rule
                    </button>
                  </div>
                  {rules.map((rule, index) => (
                    <div key={index} className="mb-3 grid grid-cols-[1fr_110px_2fr_auto] gap-2 items-center">
                      <input
                        placeholder="Rule ID"
                        value={rule.id}
                        onChange={(e) => changeRule(index, { id: e.target.value })}
                        className="rounded-lg bg-slate-950 px-3 py-2 text-xs text-slate-100 border border-slate-800 font-mono"
                      />
                      <select
                        value={rule.type}
                        onChange={(e) => changeRule(index, { type: e.target.value as EvaluatorRule['type'] })}
                        className="rounded-lg bg-slate-950 px-2.5 py-2 text-xs text-slate-100 border border-slate-800"
                      >
                        <option value="regex">Regex</option>
                        <option value="semantic">Semantic</option>
                        <option value="regression">Regression</option>
                      </select>
                      <input
                        placeholder={rule.type === 'regex' ? 'Regex pattern' : 'Rubric / pattern'}
                        value={rule.pattern}
                        onChange={(e) => changeRule(index, { pattern: e.target.value })}
                        className="rounded-lg bg-slate-950 px-3 py-2 text-xs text-slate-100 border border-slate-800 font-mono"
                      />
                      <button
                        onClick={() => setRules((r) => r.filter((_, i) => i !== index))}
                        className="rounded-lg bg-rose-950 px-3 py-2 text-xs font-bold text-rose-300 hover:bg-rose-900 border border-rose-800 transition"
                      >
                        ✕
                      </button>
                    </div>
                  ))}
                </div>
              </section>

              {/* Right Column: Pull Request & Generated YAML */}
              <section className="space-y-5 rounded-2xl bg-slate-900/90 p-6 border border-slate-800 shadow-lg">
                <h2 className="text-lg font-bold text-white tracking-wide">Save Suite as Pull Request</h2>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="block text-xs font-semibold text-slate-400 mb-1.5">Project ID</label>
                    <input
                      value={project}
                      onChange={(e) => setProject(e.target.value)}
                      className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 font-mono"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-semibold text-slate-400 mb-1.5">GitHub Target Repo</label>
                    <input
                      value={repository}
                      onChange={(e) => setRepository(e.target.value)}
                      placeholder="owner/repo"
                      className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 font-mono"
                    />
                  </div>
                </div>

                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1.5">Personal GitHub API Token</label>
                  <input
                    type="password"
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                    placeholder="ghp_xxxxxxxxxxxx"
                    className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 font-mono"
                  />
                </div>

                <button
                  disabled={!datasetId || !prompt || !repository}
                  onClick={saveSuiteAsPR}
                  className="w-full rounded-xl bg-cyan-400 py-3 text-xs font-black text-slate-950 hover:bg-cyan-300 disabled:opacity-50 shadow-lg shadow-cyan-500/20 transition"
                >
                  🚀 Save Suite as Pull Request
                </button>

                {saveStatus && (
                  <div className="rounded-xl bg-slate-950 p-4 text-xs font-medium text-cyan-300 break-all border border-slate-800">
                    {saveStatus}
                  </div>
                )}

                <h3 className="pt-2 text-xs font-bold uppercase tracking-wider text-slate-300">Generated YAML Config</h3>
                <pre className="max-h-80 overflow-auto rounded-xl bg-slate-950 p-4 text-xs font-mono text-emerald-400 border border-slate-800">
                  {generatedYaml}
                </pre>
              </section>
            </div>
          </div>
        )}

        {/* TAB 2: EXECUTION RUNS DASHBOARD */}
        {activeTab === 'runs' && (
          <div className="space-y-6">
            {/* Analytics Metric Cards & Pass Rate Charts */}
            <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
              {/* Pass Rate Card with Visual Donut/Ring */}
              <div className="rounded-2xl bg-slate-900/90 p-5 border border-slate-800 shadow-lg flex items-center justify-between">
                <div>
                  <p className="text-xs font-bold uppercase tracking-wider text-slate-400">Pass Rate</p>
                  <h3 className="text-2xl font-black text-white mt-1">{analytics.passRate}%</h3>
                  <p className="text-[11px] text-slate-400 mt-0.5">
                    <span className="text-emerald-400 font-bold">{analytics.passed} Passed</span> ·{' '}
                    <span className="text-rose-400 font-bold">{analytics.failed} Failed</span>
                  </p>
                </div>
                {/* SVG Radial Pass Rate Donut */}
                <div className="relative h-14 w-14 flex items-center justify-center">
                  <svg className="h-full w-full transform -rotate-90" viewBox="0 0 36 36">
                    <path
                      className="text-slate-800"
                      strokeWidth="4"
                      stroke="currentColor"
                      fill="none"
                      d="M18 2.0845 a 15.9155 15.9155 0 0 1 0 31.831 a 15.9155 15.9155 0 0 1 0 -31.831"
                    />
                    <path
                      className="text-emerald-400 transition-all duration-1000"
                      strokeDasharray={`${analytics.passRate}, 100`}
                      strokeWidth="4"
                      strokeLinecap="round"
                      stroke="currentColor"
                      fill="none"
                      d="M18 2.0845 a 15.9155 15.9155 0 0 1 0 31.831 a 15.9155 15.9155 0 0 1 0 -31.831"
                    />
                  </svg>
                  <span className="absolute text-[10px] font-black text-emerald-400">{analytics.passRate}%</span>
                </div>
              </div>

              {/* Total Runs */}
              <div className="rounded-2xl bg-slate-900/90 p-5 border border-slate-800 shadow-lg">
                <p className="text-xs font-bold uppercase tracking-wider text-slate-400">Total Benchmark Runs</p>
                <h3 className="text-2xl font-black text-cyan-400 mt-1">{analytics.total}</h3>
                <p className="text-[11px] text-slate-400 mt-0.5">Source: {dataSource === 'postgres' ? 'Postgres DB' : 'Local Storage'}</p>
              </div>

              {/* Average Score */}
              <div className="rounded-2xl bg-slate-900/90 p-5 border border-slate-800 shadow-lg">
                <p className="text-xs font-bold uppercase tracking-wider text-slate-400">Average Weighted Score</p>
                <h3 className="text-2xl font-black text-white mt-1">{analytics.avgScore} <span className="text-xs text-slate-400 font-normal">/ 1.00</span></h3>
                {/* Score Bar */}
                <div className="w-full bg-slate-950 rounded-full h-1.5 mt-2 overflow-hidden border border-slate-800">
                  <div
                    className="bg-cyan-400 h-full rounded-full transition-all"
                    style={{ width: `${Math.min(analytics.avgScore * 100, 100)}%` }}
                  />
                </div>
              </div>

              {/* Average Latency & Spend */}
              <div className="rounded-2xl bg-slate-900/90 p-5 border border-slate-800 shadow-lg">
                <p className="text-xs font-bold uppercase tracking-wider text-slate-400">Avg Latency & Spend</p>
                <h3 className="text-2xl font-black text-white mt-1">{analytics.avgLatency} <span className="text-xs text-slate-400 font-normal">ms</span></h3>
                <p className="text-[11px] text-slate-400 mt-0.5">Total Spend: <span className="font-mono text-emerald-400">${analytics.totalCost}</span></p>
              </div>
            </div>

            {/* Data Source Selector & Run Folder Picker Bar */}
            <div className="flex flex-wrap items-center justify-between rounded-2xl bg-slate-900/90 p-5 border border-slate-800 shadow-lg gap-4">
              <div>
                <h2 className="text-lg font-bold text-white tracking-wide">Execution Runs History</h2>
                <p className="text-xs text-slate-400 mt-0.5">Filter benchmark runs or load local run JSON files</p>
              </div>

              {/* Data Source Toggle */}
              <div className="flex flex-wrap items-center gap-3">
                <span className="text-xs font-semibold text-slate-400">Data Source:</span>
                <div className="flex rounded-xl bg-slate-950 p-1.5 border border-slate-800">
                  <button
                    onClick={() => setDataSource('postgres')}
                    className={`px-4 py-2 text-xs font-bold rounded-lg transition-all ${
                      dataSource === 'postgres'
                        ? 'bg-cyan-400 text-slate-950 shadow-md shadow-cyan-500/10'
                        : 'text-slate-300 hover:text-white'
                    }`}
                  >
                    PostgreSQL DB
                  </button>
                  <button
                    onClick={() => setDataSource('local')}
                    className={`px-4 py-2 text-xs font-bold rounded-lg transition-all ${
                      dataSource === 'local'
                        ? 'bg-cyan-400 text-slate-950 shadow-md shadow-cyan-500/10'
                        : 'text-slate-300 hover:text-white'
                    }`}
                  >
                    Local / Client Folder
                  </button>
                </div>

                {/* Show Folder/File Pickers ONLY when Data Source is Local */}
                {dataSource === 'local' && (
                  <div className="flex items-center space-x-2">
                    <label className="cursor-pointer rounded-xl bg-cyan-950 px-4 py-2.5 text-xs font-bold text-cyan-300 hover:bg-cyan-900 border border-cyan-800 transition flex items-center space-x-1.5 shadow">
                      <span>📁 Open Local Run Folder</span>
                      <input
                        type="file"
                        // @ts-ignore
                        webkitdirectory=""
                        directory=""
                        multiple
                        onChange={handleLocalRunFileUpload}
                        className="hidden"
                      />
                    </label>
                    <label className="cursor-pointer rounded-xl bg-slate-800 px-3.5 py-2.5 text-xs font-bold text-slate-200 hover:bg-slate-700 border border-slate-700 transition flex items-center space-x-1.5 shadow">
                      <span>📄 Pick .json File</span>
                      <input
                        type="file"
                        accept=".json"
                        multiple
                        onChange={handleLocalRunFileUpload}
                        className="hidden"
                      />
                    </label>
                  </div>
                )}

                <button
                  onClick={fetchRuns}
                  className="rounded-xl bg-slate-800 px-4 py-2.5 text-xs font-semibold text-cyan-300 hover:bg-slate-700 border border-slate-700 transition"
                >
                  🔄 Refresh
                </button>
              </div>
            </div>

            {/* Runs Table */}
            <div className="overflow-hidden rounded-2xl border border-slate-800 bg-slate-900/90 shadow-lg">
              {loadingRuns ? (
                <div className="p-12 text-center text-xs font-medium text-slate-400">Loading evaluation runs…</div>
              ) : runsList.length === 0 ? (
                <div className="p-12 text-center text-xs font-medium text-slate-400">
                  {dataSource === 'postgres'
                    ? 'No evaluation runs found in Postgres DB.'
                    : 'No local evaluation runs found. Click "Open Local Run Folder" or "Pick .json File".'}
                </div>
              ) : (
                <table className="w-full text-left text-xs">
                  <thead className="border-b border-slate-800 bg-slate-950/80 font-bold uppercase tracking-wider text-slate-400">
                    <tr>
                      <th className="px-5 py-4">Timestamp</th>
                      <th className="px-5 py-4">Run ID</th>
                      <th className="px-5 py-4">Dataset</th>
                      <th className="px-5 py-4">Status</th>
                      <th className="px-5 py-4">Score</th>
                      <th className="px-5 py-4">Avg Latency</th>
                      <th className="px-5 py-4">Cost (USD)</th>
                      <th className="px-5 py-4 text-right">Actions</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-800/60 text-slate-200">
                    {runsList.map((run) => (
                      <tr key={run.run_id} className="hover:bg-slate-800/40 transition">
                        <td className="px-5 py-4 font-mono text-slate-400">
                          {new Date(run.timestamp).toLocaleString()}
                        </td>
                        <td className="px-5 py-4 font-mono text-cyan-400 font-semibold">{run.run_id}</td>
                        <td className="px-5 py-4 font-bold text-white">{run.dataset_id}</td>
                        <td className="px-5 py-4">
                          {run.telemetry?.passed ? (
                            <span className="inline-flex items-center rounded-lg bg-emerald-950/80 px-3 py-1 text-xs font-bold text-emerald-400 border border-emerald-800">
                              ✓ PASSED
                            </span>
                          ) : (
                            <span className="inline-flex items-center rounded-lg bg-rose-950/80 px-3 py-1 text-xs font-bold text-rose-400 border border-rose-800">
                              ✕ FAILED
                            </span>
                          )}
                        </td>
                        <td className="px-5 py-4 font-bold text-white">
                          {(run.telemetry?.overall_score ?? 0).toFixed(2)}
                        </td>
                        <td className="px-5 py-4 font-mono text-slate-300">{run.telemetry?.avg_latency_ms ?? 0} ms</td>
                        <td className="px-5 py-4 font-mono text-slate-300">${(run.telemetry?.cost_usd ?? 0).toFixed(4)}</td>
                        <td className="px-5 py-4 text-right">
                          <button
                            onClick={() => setSelectedRunJson(run)}
                            className="rounded-lg bg-slate-800 px-3.5 py-1.5 text-xs font-bold text-cyan-300 hover:bg-slate-700 border border-slate-700 transition"
                          >
                            🔍 View JSON
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </div>
          </div>
        )}
      </div>

      {/* JSON Viewer Modal */}
      {selectedRunJson && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-md p-4">
          <div className="w-full max-w-4xl rounded-2xl border border-slate-800 bg-slate-900 p-6 shadow-2xl space-y-4">
            <div className="flex items-center justify-between border-b border-slate-800 pb-3">
              <div>
                <h3 className="text-base font-bold text-white">Execution Run Detail (JSON)</h3>
                <p className="text-xs font-mono text-cyan-400 mt-0.5">{selectedRunJson.run_id}</p>
              </div>
              <button
                onClick={() => setSelectedRunJson(null)}
                className="rounded-lg bg-slate-800 px-3 py-1.5 text-xs font-bold text-slate-300 hover:bg-slate-700 border border-slate-700"
              >
                ✕ Close
              </button>
            </div>
            <pre className="max-h-[60vh] overflow-auto rounded-xl bg-slate-950 p-4 text-xs font-mono text-cyan-300 border border-slate-800">
              {JSON.stringify(selectedRunJson, null, 2)}
            </pre>
          </div>
        </div>
      )}

      {/* User Auth / Manage Keys Modal */}
      {showAuthModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-md p-4">
          <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900 p-6 shadow-2xl space-y-5">
            <div className="flex items-center justify-between border-b border-slate-800 pb-3">
              <h3 className="text-base font-bold text-white">
                {loggedInUser ? 'User Profile & Personal Keys' : 'Authentication'}
              </h3>
              {loggedInUser && (
                <button
                  onClick={() => setShowAuthModal(false)}
                  className="rounded-lg bg-slate-800 px-2.5 py-1 text-xs font-bold text-slate-300 hover:bg-slate-700"
                >
                  ✕
                </button>
              )}
            </div>

            {!loggedInUser ? (
              <div className="space-y-4">
                {/* Auth Mode Toggle Tabs */}
                <div className="flex rounded-xl bg-slate-950 p-1 border border-slate-800">
                  <button
                    type="button"
                    onClick={() => {
                      setAuthMode('login');
                      setAuthStatus('');
                    }}
                    className={`w-1/3 py-2 text-xs font-bold rounded-lg transition-all ${
                      authMode === 'login'
                        ? 'bg-cyan-400 text-slate-950 shadow-md'
                        : 'text-slate-400 hover:text-white'
                    }`}
                  >
                    Log In
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setAuthMode('register');
                      setAuthStatus('');
                    }}
                    className={`w-1/3 py-2 text-xs font-bold rounded-lg transition-all ${
                      authMode === 'register'
                        ? 'bg-cyan-400 text-slate-950 shadow-md'
                        : 'text-slate-400 hover:text-white'
                    }`}
                  >
                    Register
                  </button>
                  <button
                    type="button"
                    onClick={() => {
                      setAuthMode('forgot');
                      setAuthStatus('');
                    }}
                    className={`w-1/3 py-2 text-xs font-bold rounded-lg transition-all ${
                      authMode === 'forgot'
                        ? 'bg-cyan-400 text-slate-950 shadow-md'
                        : 'text-slate-400 hover:text-white'
                    }`}
                  >
                    Reset Pass
                  </button>
                </div>

                {authMode !== 'forgot' ? (
                  <>
                    <div>
                      <label className="block text-xs font-semibold text-slate-400 mb-1">Username</label>
                      <input
                        value={username}
                        onChange={(e) => setUsername(e.target.value)}
                        placeholder="Enter username"
                        className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 focus:border-cyan-400 focus:outline-none font-mono"
                      />
                    </div>
                    <div>
                      <div className="flex items-center justify-between mb-1">
                        <label className="text-xs font-semibold text-slate-400">Password</label>
                        {authMode === 'login' && (
                          <button
                            type="button"
                            onClick={() => {
                              setAuthMode('forgot');
                              setAuthStatus('');
                            }}
                            className="text-[11px] font-semibold text-cyan-400 hover:underline"
                          >
                            Forgot password?
                          </button>
                        )}
                      </div>
                      <input
                        type="password"
                        value={password}
                        onChange={(e) => setPassword(e.target.value)}
                        placeholder="Enter password"
                        className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 focus:border-cyan-400 focus:outline-none font-mono"
                      />
                    </div>

                    {authMode === 'register' && (
                      <>
                        <div>
                          <label className="block text-xs font-semibold text-slate-400 mb-1">Project ID (Optional)</label>
                          <input
                            value={project}
                            onChange={(e) => setProject(e.target.value)}
                            placeholder="default"
                            className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 font-mono"
                          />
                        </div>
                        <div>
                          <label className="block text-xs font-semibold text-slate-400 mb-1">GitHub API Token (Optional)</label>
                          <input
                            type="password"
                            value={token}
                            onChange={(e) => setToken(e.target.value)}
                            placeholder="ghp_xxxxxxxxxxxx"
                            className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 font-mono"
                          />
                        </div>
                      </>
                    )}

                    <button
                      type="button"
                      onClick={() => handleLogin(authMode === 'register')}
                      className="w-full rounded-xl bg-cyan-400 py-3 text-xs font-black text-slate-950 hover:bg-cyan-300 shadow-lg shadow-cyan-500/20 transition mt-2"
                    >
                      {authMode === 'login' ? '🔐 Log In' : '✨ Create Account'}
                    </button>
                  </>
                ) : (
                  <div className="space-y-3">
                    <div>
                      <label className="block text-xs font-semibold text-slate-400 mb-1">Account Username</label>
                      <input
                        value={username}
                        onChange={(e) => setUsername(e.target.value)}
                        placeholder="Enter username"
                        className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 focus:border-cyan-400 focus:outline-none font-mono"
                      />
                    </div>

                    <button
                      type="button"
                      onClick={handleRequestReset}
                      className="w-full rounded-xl bg-slate-800 py-2.5 text-xs font-bold text-cyan-300 hover:bg-slate-700 border border-slate-700 transition"
                    >
                      🔑 Request Reset Token
                    </button>

                    <div className="pt-2 border-t border-slate-800/80 space-y-3">
                      <div>
                        <label className="block text-xs font-semibold text-slate-400 mb-1">Reset Token</label>
                        <input
                          value={resetTokenInput}
                          onChange={(e) => setResetTokenInput(e.target.value)}
                          placeholder="Paste reset token here"
                          className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 focus:border-cyan-400 focus:outline-none font-mono"
                        />
                      </div>
                      <div>
                        <label className="block text-xs font-semibold text-slate-400 mb-1">New Password</label>
                        <input
                          type="password"
                          value={newPasswordInput}
                          onChange={(e) => setNewPasswordInput(e.target.value)}
                          placeholder="Enter new password"
                          className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 focus:border-cyan-400 focus:outline-none font-mono"
                        />
                      </div>

                      <button
                        type="button"
                        onClick={handleResetPassword}
                        className="w-full rounded-xl bg-cyan-400 py-3 text-xs font-black text-slate-950 hover:bg-cyan-300 shadow-lg shadow-cyan-500/20 transition"
                      >
                        🔐 Update Password
                      </button>
                    </div>
                  </div>
                )}
              </div>
            ) : (
              <div className="space-y-4">
                <p className="text-xs font-bold text-emerald-400">Logged in as @{loggedInUser}</p>
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">Project ID</label>
                  <input
                    value={project}
                    onChange={(e) => setProject(e.target.value)}
                    className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 font-mono"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">GitHub Personal Token</label>
                  <input
                    type="password"
                    value={token}
                    onChange={(e) => setToken(e.target.value)}
                    className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 font-mono"
                  />
                </div>
                <div>
                  <label className="block text-xs font-semibold text-slate-400 mb-1">GitHub Target Repository</label>
                  <input
                    value={repository}
                    onChange={(e) => setRepository(e.target.value)}
                    className="w-full rounded-xl bg-slate-950 px-4 py-2.5 text-xs text-slate-100 border border-slate-800 font-mono"
                  />
                </div>
                <button
                  onClick={handleSaveKeys}
                  className="w-full rounded-xl bg-cyan-400 py-2.5 text-xs font-black text-slate-950 hover:bg-cyan-300 shadow-md shadow-cyan-500/20"
                >
                  Save Keys in Postgres DB
                </button>
                <button
                  onClick={() => {
                    localStorage.clear();
                    setLoggedInUser(null);
                    setUserId(null);
                  }}
                  className="w-full rounded-xl bg-rose-950 py-2.5 text-xs font-bold text-rose-300 hover:bg-rose-900 border border-rose-800"
                >
                  Log Out
                </button>
              </div>
            )}

            {authStatus && <p className="text-xs font-medium text-cyan-300 break-words">{authStatus}</p>}
          </div>
        </div>
      )}
    </main>
  );
}
