/**
 * Generate JSON output files
 */

import * as fs from "fs";
import * as path from "path";
import { TestVAA, GuardianSet } from "../types";

export interface VAAOutput {
  generatedAt: string;
  generatorVersion: string;
  governanceAction: string;
  totalTests: number;
  testCases: TestVAA[];
}

/**
 * Write VAAs to JSON file
 */
export function writeVAAsToJSON(
  vaas: TestVAA[],
  action: string,
  outputDir: string
): void {
  const output: VAAOutput = {
    generatedAt: new Date().toISOString(),
    generatorVersion: "1.0.0",
    governanceAction: action,
    totalTests: vaas.length,
    testCases: vaas,
  };

  const filename = `${action.toLowerCase().replace(/ /g, "_")}_vaas.json`;
  const filepath = path.join(outputDir, filename);

  fs.writeFileSync(filepath, JSON.stringify(output, null, 2));
  console.log(`✅ Generated ${vaas.length} VAAs → ${filepath}`);
}

/**
 * Write guardian keys to JSON file
 */
export function writeGuardianKeysToJSON(
  guardianSet: GuardianSet,
  outputDir: string
): void {
  const filepath = path.join(outputDir, "guardian_keys.json");
  fs.writeFileSync(filepath, JSON.stringify(guardianSet, null, 2));
  console.log(`✅ Generated guardian keys → ${filepath}`);
}
