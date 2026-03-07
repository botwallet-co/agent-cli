#!/usr/bin/env node
// =============================================================================
// Botwallet CLI npm postinstall script
// =============================================================================
// Downloads the appropriate binary for the user's platform.
// =============================================================================

const https = require('https');
const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

const PACKAGE_VERSION = require('../package.json').version;
const GITHUB_RELEASE_URL = `https://github.com/botwallet-co/agent-cli/releases/download/v${PACKAGE_VERSION}`;

// Platform mapping
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

function getBinaryName() {
  const platform = getPlatform();
  const arch = getArch();
  const ext = platform === 'windows' ? '.exe' : '';
  return `botwallet_${PACKAGE_VERSION}_${platform}_${arch}${ext}`;
}

function getArchiveName() {
  const platform = getPlatform();
  const arch = getArch();
  const ext = platform === 'windows' ? 'zip' : 'tar.gz';
  return `botwallet_${PACKAGE_VERSION}_${platform}_${arch}.${ext}`;
}

function downloadFile(url, dest) {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(dest);
    
    const request = https.get(url, (response) => {
      // Handle redirects
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
      fs.unlink(dest, () => {}); // Delete partial file
      reject(err);
    });
  });
}

function extractArchive(archivePath, destDir) {
  const platform = getPlatform();
  
  if (platform === 'windows') {
    // Use PowerShell to extract zip
    execSync(`powershell -Command "Expand-Archive -Path '${archivePath}' -DestinationPath '${destDir}' -Force"`, {
      stdio: 'inherit'
    });
  } else {
    // Use tar for tar.gz
    execSync(`tar -xzf "${archivePath}" -C "${destDir}"`, {
      stdio: 'inherit'
    });
  }
}

async function install() {
  console.log('Installing Botwallet CLI...');
  
  const binDir = path.join(__dirname, '..', 'bin');
  const tmpDir = path.join(__dirname, '..', 'tmp');
  
  // Create directories
  if (!fs.existsSync(binDir)) {
    fs.mkdirSync(binDir, { recursive: true });
  }
  if (!fs.existsSync(tmpDir)) {
    fs.mkdirSync(tmpDir, { recursive: true });
  }
  
  try {
    const archiveName = getArchiveName();
    const archiveUrl = `${GITHUB_RELEASE_URL}/${archiveName}`;
    const archivePath = path.join(tmpDir, archiveName);
    
    console.log(`Downloading ${archiveName}...`);
    await downloadFile(archiveUrl, archivePath);
    
    console.log('Extracting...');
    extractArchive(archivePath, tmpDir);
    
    // Move binary to bin directory
    const platform = getPlatform();
    const srcBinary = path.join(tmpDir, platform === 'windows' ? 'botwallet.exe' : 'botwallet');
    const destBinary = path.join(binDir, platform === 'windows' ? 'botwallet.exe' : 'botwallet');
    
    // Handle case where binary is inside the archive with full name
    if (!fs.existsSync(srcBinary)) {
      const binaryName = getBinaryName();
      const altSrcBinary = path.join(tmpDir, binaryName);
      if (fs.existsSync(altSrcBinary)) {
        fs.renameSync(altSrcBinary, destBinary);
      } else {
        throw new Error(`Binary not found in archive: ${srcBinary} or ${altSrcBinary}`);
      }
    } else {
      fs.renameSync(srcBinary, destBinary);
    }
    
    // Make executable on Unix
    if (platform !== 'windows') {
      fs.chmodSync(destBinary, 0o755);
    }
    
    // Clean up
    fs.rmSync(tmpDir, { recursive: true });
    
    console.log('Botwallet CLI installed successfully!');

    try {
      const npmPrefix = execSync('npm prefix -g', { encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] }).trim();
      const isWindows = process.platform === 'win32';
      const npmBinDir = isWindows ? npmPrefix : path.join(npmPrefix, 'bin');

      const normalize = (p) => path.resolve(p).replace(/[\\/]+$/, '');
      const caseSensitive = !isWindows;
      const npmBinNorm = normalize(npmBinDir);

      const pathDirs = (process.env.PATH || '').split(path.delimiter);
      const inPath = pathDirs.some(d => {
        const norm = normalize(d);
        return caseSensitive ? norm === npmBinNorm : norm.toLowerCase() === npmBinNorm.toLowerCase();
      });

      if (!inPath) {
        const fullCmd = isWindows ? path.join(npmBinDir, 'botwallet.cmd') : path.join(npmBinDir, 'botwallet');
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

// Run installation
install();








