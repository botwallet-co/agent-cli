#!/usr/bin/env node
// =============================================================================
// Botwallet CLI npm install script
// =============================================================================
// Downloads the appropriate Go binary for the user's platform.
//
// Used in two ways:
//   1. Directly as a postinstall script (npm runs this after package install)
//   2. Imported by bin/botwallet as a fallback when binary is missing
//
// The archive's SHA-256 is checked against the release's checksums.txt
// before anything is extracted. A checksums.txt shipped inside the npm
// package is used when present (npm checks the package's own integrity);
// otherwise it is downloaded from the same GitHub release.
// =============================================================================

const https = require('https');
const fs = require('fs');
const path = require('path');
const crypto = require('crypto');
const { execSync, execFileSync } = require('child_process');

const PACKAGE_VERSION = require('../package.json').version;
const GITHUB_RELEASE_URL = `https://github.com/botwallet-co/agent-cli/releases/download/v${PACKAGE_VERSION}`;
const BUNDLED_CHECKSUMS = path.join(__dirname, '..', 'checksums.txt');

const PLATFORM_MAP = {
  darwin: 'darwin',
  linux: 'linux',
  win32: 'windows'
};

const ARCH_MAP = {
  x64: 'amd64',
  arm64: 'arm64'
};

function getPlatform() {
  const platform = PLATFORM_MAP[process.platform];
  if (!platform) {
    throw new Error(`Unsupported platform: ${process.platform}`);
  }
  return platform;
}

function getArch() {
  const arch = ARCH_MAP[process.arch];
  if (!arch) {
    throw new Error(`Unsupported architecture: ${process.arch}`);
  }
  return arch;
}

function getArchiveName() {
  const platform = getPlatform();
  const arch = getArch();
  const ext = platform === 'windows' ? 'zip' : 'tar.gz';
  return `botwallet_${PACKAGE_VERSION}_${platform}_${arch}.${ext}`;
}

function getBinaryName() {
  const platform = getPlatform();
  const arch = getArch();
  const ext = platform === 'windows' ? '.exe' : '';
  return `botwallet_${PACKAGE_VERSION}_${platform}_${arch}${ext}`;
}

// Where the Go binary is installed. It must never be bin/botwallet: that is
// the Node launcher npm links as the `botwallet` command, and the botwallet
// and botwallet-cli wrapper packages require() it.
function nativeBinaryPath(binDir) {
  return path.join(binDir, process.platform === 'win32' ? 'botwallet.exe' : 'botwallet-native');
}

function downloadFile(url, dest) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(dest);
    file.on('error', (err) => {
      fs.unlink(dest, () => {});
      reject(err);
    });

    const request = https.get(url, (response) => {
      if (response.statusCode === 302 || response.statusCode === 301) {
        file.close();
        fs.unlinkSync(dest);
        return downloadFile(response.headers.location, dest).then(resolve).catch(reject);
      }

      if (response.statusCode !== 200) {
        file.close();
        fs.unlinkSync(dest);
        reject(new Error(`Failed to download: HTTP ${response.statusCode}`));
        return;
      }

      response.pipe(file);

      file.on('finish', () => {
        file.close();
        resolve();
      });
    });

    request.on('error', (err) => {
      fs.unlink(dest, () => {});
      reject(err);
    });
  });
}

// parseChecksums reads a GoReleaser checksums.txt ("<sha256>  <file name>"
// per line) into { fileName: sha256 }.
function parseChecksums(text) {
  const sums = {};
  for (const line of text.split(/\r?\n/)) {
    const match = line.trim().match(/^([0-9a-fA-F]{64})\s+\*?(\S.*)$/);
    if (match) {
      sums[match[2].trim()] = match[1].toLowerCase();
    }
  }
  return sums;
}

async function loadChecksums(tmpDir) {
  if (fs.existsSync(BUNDLED_CHECKSUMS)) {
    return parseChecksums(fs.readFileSync(BUNDLED_CHECKSUMS, 'utf8'));
  }
  const dest = path.join(tmpDir, 'checksums.txt');
  await downloadFile(`${GITHUB_RELEASE_URL}/checksums.txt`, dest);
  return parseChecksums(fs.readFileSync(dest, 'utf8'));
}

// verifyArchive throws unless the file at archivePath has the SHA-256 that
// checksums.txt lists for archiveName.
function verifyArchive(archivePath, archiveName, sums) {
  const expected = sums[archiveName];
  if (!expected) {
    throw new Error(`checksums.txt has no entry for ${archiveName}, so the download cannot be checked. Nothing was installed.`);
  }
  const actual = crypto.createHash('sha256').update(fs.readFileSync(archivePath)).digest('hex');
  if (actual !== expected) {
    throw new Error(`Checksum mismatch for ${archiveName} (expected ${expected}, got ${actual}). ` +
      'The download is corrupt or was changed. Nothing was installed.');
  }
}

// The paths go in as arguments or environment variables, never into a
// command string, so quotes or spaces in the install path can't break it.
// On Windows, .NET's ZipFile takes the paths literally (Expand-Archive reads
// [ and ] as wildcards), and Stop makes any failure exit non-zero.
function extractArchive(archivePath, destDir) {
  const platform = getPlatform();

  if (platform === 'windows') {
    execFileSync('powershell', [
      '-NoProfile', '-NonInteractive', '-Command',
      "$ErrorActionPreference = 'Stop'; Add-Type -AssemblyName System.IO.Compression.FileSystem; " +
      '[IO.Compression.ZipFile]::ExtractToDirectory($env:BOTWALLET_ARCHIVE, $env:BOTWALLET_DEST)'
    ], {
      stdio: 'pipe',
      env: Object.assign({}, process.env, { BOTWALLET_ARCHIVE: archivePath, BOTWALLET_DEST: destDir })
    });
  } else {
    execFileSync('tar', ['-xzf', archivePath, '-C', destDir], { stdio: 'pipe' });
  }
}

// Core download function — used by both postinstall and bin wrapper fallback.
// `binDir` is where the Go binary should end up.
// `options.log` controls output destination (default: console.log = stdout).
async function downloadBinary(binDir, options = {}) {
  const log = options.log || console.log.bind(console);

  fs.mkdirSync(binDir, { recursive: true });
  // A folder per run, so first runs started at the same time (agents often run
  // commands in parallel) don't delete each other's downloads
  const tmpDir = fs.mkdtempSync(path.join(__dirname, '..', 'tmp-'));

  const archiveName = getArchiveName();
  const archiveUrl = `${GITHUB_RELEASE_URL}/${archiveName}`;
  const archivePath = path.join(tmpDir, archiveName);
  const platform = getPlatform();
  const destBinary = nativeBinaryPath(binDir);

  try {
    log(`Downloading ${archiveName}...`);
    await downloadFile(archiveUrl, archivePath);

    log('Verifying checksum...');
    verifyArchive(archivePath, archiveName, await loadChecksums(tmpDir));

    log('Extracting...');
    extractArchive(archivePath, tmpDir);

    // The archive holds botwallet(.exe); older releases used the long name
    const archiveBinaryName = platform === 'windows' ? 'botwallet.exe' : 'botwallet';
    const srcBinary = [archiveBinaryName, getBinaryName()]
      .map((name) => path.join(tmpDir, name))
      .find((p) => fs.existsSync(p));
    if (!srcBinary) {
      throw new Error(`Binary not found in archive. Expected: ${archiveBinaryName} or ${getBinaryName()}`);
    }
    try {
      fs.renameSync(srcBinary, destBinary);
    } catch (err) {
      // Another run may have installed it first; on Windows a running
      // botwallet.exe can't be replaced
      if (!fs.existsSync(destBinary)) throw err;
    }

    if (platform !== 'windows') {
      fs.chmodSync(destBinary, 0o755);
    }
  } finally {
    // A cleanup failure must not hide the real result
    try {
      fs.rmSync(tmpDir, { recursive: true, force: true, maxRetries: 3 });
    } catch {}
  }

  // Verify the binary is real (not empty)
  const stat = fs.statSync(destBinary);
  if (stat.size < 1000) {
    fs.unlinkSync(destBinary);
    throw new Error('Downloaded binary appears corrupt (too small). Try again.');
  }

  return destBinary;
}

// Postinstall entry point — runs when `npm install` triggers this script directly.
async function postinstall() {
  console.log('Installing Botwallet CLI...');

  const binDir = path.join(__dirname, '..', 'bin');

  try {
    await downloadBinary(binDir);
    console.log('Botwallet CLI installed successfully!');

    try {
      const npmPrefix = execSync('npm prefix -g', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }).trim();
      const isWin = process.platform === 'win32';
      const npmBinDir = isWin ? npmPrefix : path.join(npmPrefix, 'bin');

      const normalize = (p) => path.resolve(p).replace(/[\\/]+$/, '');
      const caseSensitive = !isWin;
      const npmBinNorm = normalize(npmBinDir);

      const pathDirs = (process.env.PATH || '').split(path.delimiter);
      const inPath = pathDirs.some(d => {
        const norm = normalize(d);
        return caseSensitive ? norm === npmBinNorm : norm.toLowerCase() === npmBinNorm.toLowerCase();
      });

      if (!inPath) {
        const fullCmd = isWin ? path.join(npmBinDir, 'botwallet.cmd') : path.join(npmBinDir, 'botwallet');
        console.log('');
        console.log(`NOTE: npm global bin directory is not in your PATH.`);
        console.log(`If "botwallet" is not recognized as a command, use the full path:`);
        console.log(`  ${fullCmd}`);
        console.log(`To fix permanently, add to your PATH: ${npmBinDir}`);
      } else {
        console.log('Run "botwallet --help" to get started.');
      }
    } catch {
      console.log('Run "botwallet --help" to get started.');
    }

  } catch (error) {
    console.error('Installation failed:', error.message);
    console.error('');
    console.error('You can manually download the binary from:');
    console.error(`https://github.com/botwallet-co/agent-cli/releases/tag/v${PACKAGE_VERSION}`);
    process.exit(1);
  }
}

module.exports = { downloadBinary, nativeBinaryPath, parseChecksums, verifyArchive };

if (require.main === module) {
  postinstall();
}
