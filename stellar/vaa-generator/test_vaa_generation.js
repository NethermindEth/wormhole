const fs = require('fs');

// Read the generated contract upgrade VAAs
const data = JSON.parse(fs.readFileSync('generated/contract_upgrade_vaas.json', 'utf8'));

// Find the valid_stellar_specific test case
const testCase = data.testCases.find(tc => tc.name === 'valid_stellar_specific');

if (!testCase) {
  console.error('Test case not found!');
  process.exit(1);
}

console.log('Test case found:', testCase.name);
console.log('VAA hex length:', testCase.vaa.hex.length);
console.log('VAA base64 length:', testCase.vaa.base64.length);
console.log('Payload:', testCase.payload);
console.log('\nFirst 100 hex chars:', testCase.vaa.hex.substring(0, 100));

// Decode and verify structure
const vaaHex = testCase.vaa.hex;
const buffer = Buffer.from(vaaHex, 'hex');

console.log('\n=== VAA Structure ===');
console.log('Total length:', buffer.length);
console.log('Version:', buffer.readUInt8(0));
console.log('Guardian Set Index:', buffer.readUInt32BE(1));
console.log('Num Signatures:', buffer.readUInt8(5));
