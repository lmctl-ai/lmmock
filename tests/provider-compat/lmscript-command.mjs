import fs from 'node:fs';
import path from 'node:path';

/** Resolve npm's Windows command shim to the package's JavaScript entrypoint. */
export function parseNpmCmdShim(shimPath, contents) {
  const match = /%dp0%[\\/]([^"\r\n]+)"\s+%\*/i.exec(contents);
  if (!match) throw new Error('unsupported npm lmscript shim');
  const target = match[1].replace(/[\\/]/g, path.sep);
  return {command: process.execPath, args: [path.resolve(path.dirname(shimPath), target)]};
}

export function windowsCommandCandidates(command, env = process.env) {
  const winPath = path.win32;
  const extensions = (env.PATHEXT || '.COM;.EXE;.BAT;.CMD').split(';');
  const explicit = /[\\/]/.test(command) || winPath.isAbsolute(command);
  const directories = explicit ? [''] : (env.PATH ?? env.Path ?? '').split(winPath.delimiter);
  const names = winPath.extname(command) ? [command] : [...extensions.map(ext => command + ext), command];
  const candidates = [];
  for (const directory of directories) {
    for (const name of names) candidates.push(directory ? winPath.join(directory, name) : name);
  }
  return candidates;
}

export function resolveLmscriptCommand(command = process.env.LMSCRIPT_BIN || 'lmscript', env = process.env, platform = process.platform) {
  if (platform !== 'win32') return {command, args: []};
  const shimPath = windowsCommandCandidates(command, env).find(candidate => fs.existsSync(candidate));
  if (!shimPath) throw new Error(`lmscript executable not found: ${command}`);
  if (/\.(cmd|bat)$/i.test(shimPath)) return parseNpmCmdShim(shimPath, fs.readFileSync(shimPath, 'utf8'));
  return {command: shimPath, args: []};
}
