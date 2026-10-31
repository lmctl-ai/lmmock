// Process manifests own their stdio peers; never start an HTTP listener for them.
import path from 'node:path';
import fs from 'node:fs';
import {expand} from './run.mjs';

export async function runProcessSuite(loaded, tempDir, {executeProcess, checkExpectation}) {
  const {manifest, root} = loaded;
  if (manifest.server.kind !== 'stdio') throw new Error('owned process suite requires stdio manifest');
  const vars = {
    repo_root: root,
    tmpdir: tempDir,
    base_url: '',
    lmmock_url: '',
    node_binary: process.execPath,
    server_url_env: '',
  };
  let passed = 0;
  const failures = [];
  for (const testCase of manifest.cases) {
    if (testCase.request.kind !== 'process') throw new Error('stdio manifest requires process cases');
    try {
      const executable = expand(testCase.request.executable, vars);
      if (executable !== 'node' && executable !== process.execPath) {
        throw new Error('stdio process case must use a repo-owned Node harness; refusing a bare provider command');
      }
      const argv = testCase.request.argv.map(arg => expand(arg, vars));
      const entry = fs.realpathSync(path.resolve(root, argv[0]));
      const tests = fs.realpathSync(path.join(root, 'tests'));
      if (!entry.startsWith(tests + path.sep)) throw new Error('process harness escapes fixture tests directory');
      const request = {...testCase.request, executable: process.execPath, argv: [entry, ...argv.slice(1)]};
      const result = await executeProcess(request, root, vars);
      await checkExpectation(testCase, result, vars);
      passed++;
      console.log(`PASS ${manifest.id}/${testCase.id}`);
    } catch (error) {
      failures.push(String(error));
    }
  }
  if (failures.length) throw new Error(failures.join('\n'));
  return {passed, failed: 0};
}
