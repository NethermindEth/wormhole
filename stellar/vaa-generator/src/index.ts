/**
 * Main entry point for VAA generator
 */

import * as path from "path";
import * as fs from "fs";
// Note: Using getMockGuardiansForSet from guardian-sets/generator for consistency
import { generateContractUpgradeTests } from "./test-cases/contract-upgrade-tests";
import { writeVAAsToJSON, writeGuardianKeysToJSON } from "./output/json-generator";
import { GuardianSet, TestVAA } from "./types";

// Import new generators
import {
  generateAllGuardianSets,
  getGuardianSetInfo
} from "./guardian-sets/generator";
import {
  generateGuardianSetUpgradeVAA,
  generateInvalidGuardianUpgradeVAAs
} from "./test-cases/guardian-upgrade";
import {
  generateSetMessageFeeTestCases,
  generateInvalidSetMessageFeeVAAs
} from "./test-cases/set-message-fee";
import {
  generateTransferFeesTestCases,
  generateInvalidTransferFeesVAAs
} from "./test-cases/transfer-fees";
import {
  generateTransferFeesTestCasesTestnet
} from "./test-cases/transfer-fees-testnet";
import {
  generateAllEdgeCaseVAAs
} from "./test-cases/edge-cases";

const GENERATED_DIR = path.join(__dirname, "..", "generated");

/**
 * Ensure output directory exists
 */
function ensureOutputDir(): void {
  if (!fs.existsSync(GENERATED_DIR)) {
    fs.mkdirSync(GENERATED_DIR, { recursive: true });
  }
}

/**
 * Write multiple guardian sets to JSON files
 */
function writeMultipleGuardianSets(sets: { gs0: GuardianSet, gs1: GuardianSet, gs2: GuardianSet }): void {
  // Write individual guardian set files
  const gs0Path = path.join(GENERATED_DIR, "guardian_keys_gs0.json");
  const gs1Path = path.join(GENERATED_DIR, "guardian_keys_gs1.json");
  const gs2Path = path.join(GENERATED_DIR, "guardian_keys_gs2.json");

  fs.writeFileSync(gs0Path, JSON.stringify(sets.gs0, null, 2));
  fs.writeFileSync(gs1Path, JSON.stringify(sets.gs1, null, 2));
  fs.writeFileSync(gs2Path, JSON.stringify(sets.gs2, null, 2));

  console.log(`✅ Generated Guardian Set 0 (${sets.gs0.guardians.length} guardians) → ${gs0Path}`);
  console.log(`✅ Generated Guardian Set 1 (${sets.gs1.guardians.length} guardians) → ${gs1Path}`);
  console.log(`✅ Generated Guardian Set 2 (${sets.gs2.guardians.length} guardians) → ${gs2Path}`);

  // Also write legacy guardian_keys.json (for backward compatibility with existing tests)
  writeGuardianKeysToJSON(sets.gs2, GENERATED_DIR);
}

/**
 * Main generation function
 */
function main(): void {
  console.log("🚀 Wormhole VAA Generator - Comprehensive Test Suite\n");
  console.log("=" .repeat(60));

  // Get V2 hash from command-line argument (if provided)
  const v2WasmHash = process.argv[2] || undefined;
  if (v2WasmHash) {
    console.log(`\n📦 Using V2 WASM Hash: ${v2WasmHash}`);
  }

  // Ensure output directory
  ensureOutputDir();

  // Phase 1: Generate Guardian Sets
  console.log("\n📝 Phase 1: Generating Guardian Sets...");
  console.log("-".repeat(40));
  const guardianSets = generateAllGuardianSets();

  // Display guardian set info
  const gs0Info = getGuardianSetInfo(guardianSets.gs0);
  const gs1Info = getGuardianSetInfo(guardianSets.gs1);
  const gs2Info = getGuardianSetInfo(guardianSets.gs2);

  console.log(`Guardian Set 0: ${gs0Info.numGuardians} guardians (quorum: ${gs0Info.quorum})`);
  console.log(`Guardian Set 1: ${gs1Info.numGuardians} guardians (quorum: ${gs1Info.quorum})`);
  console.log(`Guardian Set 2: ${gs2Info.numGuardians} guardians (quorum: ${gs2Info.quorum})`);

  writeMultipleGuardianSets(guardianSets);

  // Phase 2: Generate Contract Upgrade VAAs (using GS2 for current deployment)
  console.log("\n🏗️  Phase 2: Generating Contract Upgrade VAAs...");
  console.log("-".repeat(40));
  // Use GS2 from our generated guardian sets for consistency
  const { getMockGuardiansForSet } = require("./guardian-sets/generator");
  const mockGuardiansGS2 = getMockGuardiansForSet(guardianSets.gs2);
  const contractUpgradeVAAs = generateContractUpgradeTests(mockGuardiansGS2, v2WasmHash);
  writeVAAsToJSON(contractUpgradeVAAs, "contract_upgrade", GENERATED_DIR);

  // Phase 3: Generate Guardian Set Upgrade VAAs
  console.log("\n👥 Phase 3: Generating Guardian Set Upgrade VAAs...");
  console.log("-".repeat(40));
  const guardianUpgradeVAAs: TestVAA[] = [];

  // GS0 -> GS1 upgrade
  const gs0ToGs1 = generateGuardianSetUpgradeVAA(guardianSets.gs0, guardianSets.gs1);
  guardianUpgradeVAAs.push(gs0ToGs1);
  console.log(`  ✓ GS0 → GS1 upgrade VAA`);

  // GS1 -> GS2 upgrade
  const gs1ToGs2 = generateGuardianSetUpgradeVAA(guardianSets.gs1, guardianSets.gs2);
  guardianUpgradeVAAs.push(gs1ToGs2);
  console.log(`  ✓ GS1 → GS2 upgrade VAA`);

  // Invalid upgrades (using GS2 as current)
  const invalidUpgrades = generateInvalidGuardianUpgradeVAAs(guardianSets.gs2);
  guardianUpgradeVAAs.push(...invalidUpgrades);
  console.log(`  ✓ ${invalidUpgrades.length} invalid upgrade VAAs`);

  writeVAAsToJSON(guardianUpgradeVAAs, "guardian_set_upgrade", GENERATED_DIR);

  // Phase 4: Generate Set Message Fee VAAs
  console.log("\n💰 Phase 4: Generating Set Message Fee VAAs...");
  console.log("-".repeat(40));
  const setFeeVAAs: TestVAA[] = [];

  // Valid fee settings (using GS2)
  const validFees = generateSetMessageFeeTestCases(guardianSets.gs2);
  setFeeVAAs.push(...validFees);
  console.log(`  ✓ ${validFees.length} valid fee VAAs (0, 0.5, 1, 10 XLM, max u64)`);

  // Invalid fee settings
  const invalidFees = generateInvalidSetMessageFeeVAAs(guardianSets.gs2);
  setFeeVAAs.push(...invalidFees);
  console.log(`  ✓ ${invalidFees.length} invalid fee VAAs`);

  writeVAAsToJSON(setFeeVAAs, "set_message_fee", GENERATED_DIR);

  // Phase 5: Generate Transfer Fees VAAs
  console.log("\n💸 Phase 5: Generating Transfer Fees VAAs...");
  console.log("-".repeat(40));
  const transferFeesVAAs: TestVAA[] = [];

  // Valid transfers (using GS2)
  const validTransfers = generateTransferFeesTestCases(guardianSets.gs2);
  transferFeesVAAs.push(...validTransfers);
  console.log(`  ✓ ${validTransfers.length} valid transfer VAAs (0.5, 1, 10, 100 XLM)`);

  // Invalid transfers
  const invalidTransfers = generateInvalidTransferFeesVAAs(guardianSets.gs2);
  transferFeesVAAs.push(...invalidTransfers);
  console.log(`  ✓ ${invalidTransfers.length} invalid transfer VAAs`);

  writeVAAsToJSON(transferFeesVAAs, "transfer_fees", GENERATED_DIR);

  // Phase 5.5: Generate Transfer Fees VAAs for Testnet Integration
  console.log("\n💸 Phase 5.5: Generating Transfer Fees VAAs for Testnet...");
  console.log("-".repeat(40));
  const testnetTransferFeesVAAs = generateTransferFeesTestCasesTestnet(guardianSets.gs2);
  console.log(`  ✓ ${testnetTransferFeesVAAs.length} transfer VAAs for testnet integration`);
  writeVAAsToJSON(testnetTransferFeesVAAs, "transfer_fees_testnet", GENERATED_DIR);

  // Phase 6: Generate Edge Case VAAs
  console.log("\n⚠️  Phase 6: Generating Edge Case VAAs...");
  console.log("-".repeat(40));
  const edgeCaseVAAs = generateAllEdgeCaseVAAs(guardianSets.gs2);
  console.log(`  ✓ ${edgeCaseVAAs.filter(v => v.name.includes('wrong_governance')).length} wrong governance source VAAs`);
  console.log(`  ✓ ${edgeCaseVAAs.filter(v => v.name.includes('signature')).length} signature issue VAAs`);
  console.log(`  ✓ ${edgeCaseVAAs.filter(v => v.name.includes('payload')).length} malformed payload VAAs`);
  writeVAAsToJSON(edgeCaseVAAs, "edge_cases", GENERATED_DIR);

  // Summary
  console.log("\n" + "=".repeat(60));
  console.log("✨ Generation Complete!");
  console.log("=".repeat(60));

  const totalVAAs = contractUpgradeVAAs.length + guardianUpgradeVAAs.length +
                   setFeeVAAs.length + transferFeesVAAs.length + edgeCaseVAAs.length;

  console.log("\n📊 Summary:");
  console.log(`  Guardian Sets: 3 (3, 7, and 19 guardians)`);
  console.log(`  Total VAAs Generated: ${totalVAAs}`);
  console.log(`    - Contract Upgrade: ${contractUpgradeVAAs.length} VAAs`);
  console.log(`    - Guardian Set Upgrade: ${guardianUpgradeVAAs.length} VAAs`);
  console.log(`    - Set Message Fee: ${setFeeVAAs.length} VAAs`);
  console.log(`    - Transfer Fees: ${transferFeesVAAs.length} VAAs`);
  console.log(`    - Edge Cases: ${edgeCaseVAAs.length} VAAs`);

  console.log(`\n📂 Output directory: ${GENERATED_DIR}`);
  console.log("\n📄 Generated files:");
  console.log("  Guardian Sets:");
  console.log("    - guardian_keys_gs0.json (3 guardians for initial testing)");
  console.log("    - guardian_keys_gs1.json (7 guardians for upgrade testing)");
  console.log("    - guardian_keys_gs2.json (19 guardians for production testing)");
  console.log("    - guardian_keys.json (legacy file = GS2)");
  console.log("  VAA Test Cases:");
  console.log("    - contract_upgrade_vaas.json");
  console.log("    - guardian_set_upgrade_vaas.json");
  console.log("    - set_message_fee_vaas.json");
  console.log("    - transfer_fees_vaas.json");
  console.log("    - edge_cases_vaas.json");

  console.log("\n🚀 Next Steps:");
  console.log("  1. Deploy contract to Stellar testnet");
  console.log("  2. Initialize with guardian_keys_gs0.json (3 guardians)");
  console.log("  3. Test guardian set upgrades (GS0 → GS1 → GS2)");
  console.log("  4. Test all governance actions with generated VAAs");
  console.log("  5. Run edge case tests for error handling");

  console.log("=" .repeat(60) + "\n");
}

// Run
main();
