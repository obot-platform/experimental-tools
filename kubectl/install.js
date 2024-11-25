#!/usr/bin/env node

const fs = require('fs');
const path = require('path');
const https = require('https');

// Define the installation directory and kubectl path
const INSTALL_DIR = `${process.env.GPTSCRIPT_WORKSPACE_DIR}/bin`;
const KUBECTL_PATH = path.join(INSTALL_DIR, 'kubectl');

// Check if kubectl is already installed
if (fs.existsSync(KUBECTL_PATH)) {
    console.log(`kubectl is already installed at ${KUBECTL_PATH}`);
    process.exit(0);
}

// Determine the operating system and architecture
const osType = process.platform;
const arch = process.arch;

let OS_TYPE, ARCH;

// Map machine type to kubectl download URL
switch (arch) {
    case 'x64':
        ARCH = 'amd64';
        break;
    case 'arm64':
        ARCH = 'arm64';
        break;
    case 'arm':
        ARCH = 'arm';
        break;
    default:
        console.error(`Unsupported machine type: ${arch}`);
        process.exit(1);
}

// Handle different OS types
if (osType === 'linux') {
    OS_TYPE = 'linux';
} else if (osType === 'darwin') {
    OS_TYPE = 'darwin';
} else {
    console.error(`Unsupported OS type: ${osType}`);
    console.error('For Windows, consider using WSL or a compatible shell like Git Bash.');
    process.exit(1);
}

// Function to fetch URLs with redirect handling
const fetchWithRedirects = (urlToFetch, maxRedirects = 5) => {
    return new Promise((resolve, reject) => {
        const handleResponse = (response, redirectsLeft) => {
            if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
                if (redirectsLeft > 0) {
                    const redirectUrl = new URL(response.headers.location, urlToFetch).toString();
                    fetchWithRedirects(redirectUrl, redirectsLeft - 1).then(resolve).catch(reject);
                } else {
                    reject(new Error("Too many redirects"));
                }
            } else if (response.statusCode === 200) {
                let data = '';
                response.on('data', chunk => data += chunk);
                response.on('end', () => resolve(data));
            } else {
                reject(new Error(`Request failed with status code: ${response.statusCode}`));
            }
        };

        https.get(urlToFetch, response => handleResponse(response, maxRedirects))
            .on('error', reject);
    });
};

// Function to download a file with redirect handling
const downloadFileWithRedirects = (urlToDownload, outputPath, maxRedirects = 5) => {
    return new Promise((resolve, reject) => {
        const handleResponse = (response, redirectsLeft) => {
            if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
                if (redirectsLeft > 0) {
                    const redirectUrl = new URL(response.headers.location, urlToDownload).toString();
                    downloadFileWithRedirects(redirectUrl, outputPath, redirectsLeft - 1).then(resolve).catch(reject);
                } else {
                    reject(new Error("Too many redirects"));
                }
            } else if (response.statusCode === 200) {
                const file = fs.createWriteStream(outputPath);
                response.pipe(file);
                
                file.on('finish', () => {
                    file.close(() => {
                        fs.chmodSync(outputPath, '755');
                        console.log(`kubectl has been installed at ${outputPath}`);
                        resolve();
                    });
                });

                file.on('error', (error) => {
                    fs.unlinkSync(outputPath); // Remove partially downloaded file
                    reject(error);
                });
            } else {
                reject(new Error(`Request failed with status code: ${response.statusCode}`));
            }
        };

        https.get(urlToDownload, response => handleResponse(response, maxRedirects))
            .on('error', reject);
    });
};

// Function to get the latest stable kubectl version URL
const fetchKubectlURL = async () => {
    const stableVersionUrl = 'https://dl.k8s.io/release/stable.txt';
    const version = await fetchWithRedirects(stableVersionUrl);
    return `https://dl.k8s.io/release/${version.trim()}/bin/${OS_TYPE}/${ARCH}/kubectl`;
};

// Function to download and install kubectl
const downloadKubectl = async () => {
    try {
        const KUBECTL_URL = await fetchKubectlURL();
        fs.mkdirSync(INSTALL_DIR, { recursive: true });
        await downloadFileWithRedirects(KUBECTL_URL, KUBECTL_PATH);
        process.exit(0); // Exit with success code after successful download
    } catch (error) {
        console.error(`Failed to download kubectl: ${error.message}`);
        process.exit(1); // Exit with failure code if any step fails
    }
};

// Run the installation
downloadKubectl();

