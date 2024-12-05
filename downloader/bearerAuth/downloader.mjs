import { GPTScript } from '@gptscript-ai/gptscript';
import axios from 'axios';


const JWT = process.env.BEARER_AUTH_JWT;

const [, , url, outputPath] = process.argv;
if (!url || !outputPath) {
    console.error('Usage: downloader.js <url> <outputPath>');
    console.error('Example: downloader.js https://example.com/file.zip output.zip');
    process.exit(1);
}

/**
 * Downloads a file using a Bearer token for authorization and writes its content
 * using GPTScript.writeFileInWorkspace.
 *
 * @param {string} fileUrl - The URL of the file to download.
 * @param {string} token - The Bearer token for authorization.
 * @param {string} filePath - The file path where the file will be saved in the workspace.
 */
async function downloadAndWriteFile(fileUrl, token, filePath) {
    const client = new GPTScript()
    try {
        // Download the file
        const response = await axios.get(fileUrl, {
            headers: {
                Authorization: `Bearer ${token}`,
            },
            responseType: 'arraybuffer', // To get the response as a binary buffer
        });

        // Ensure the file content is received
        if (response.status === 200) {
            const content = Buffer.from(response.data, 'binary');

            // Write the content using GPTScript
            await client.writeFileInWorkspace(filePath, content);

            console.log(`File successfully written to ${filePath}`);
            process.exit(0);
        } else {
            console.error(`Failed to download file. HTTP Status: ${response.status}`);
            process.exit(1);
        }
    } catch (error) {
        console.error('Error occurred:', error.message);
        process.exit(1);
    }
}

// Example usage
(async () => {
    const fileUrl = url;
    const token = JWT;
    const filePath = `files/${outputPath}`;

    await downloadAndWriteFile(fileUrl, token, filePath);
})();
