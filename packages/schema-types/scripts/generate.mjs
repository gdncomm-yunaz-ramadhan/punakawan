#!/usr/bin/env node
import { readFileSync, writeFileSync, mkdirSync, readdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { compile } from 'json-schema-to-typescript';
import jsonSchemaToZod from 'json-schema-to-zod';

const __dirname = dirname(fileURLToPath(import.meta.url));
const protocolDir = join(__dirname, '..', '..', '..', 'protocol');
const outDir = join(__dirname, '..', 'src', 'generated');

mkdirSync(outDir, { recursive: true });

const schemaFiles = readdirSync(protocolDir)
  .filter((f) => f.endsWith('.schema.json'))
  .sort();

const banner = [
  '/* eslint-disable */',
  '/**',
  ' * Code generated from protocol/*.schema.json. DO NOT EDIT.',
  ' * Regenerate with `pnpm --filter @punakawan/schema-types generate`.',
  ' */',
].join('\n');

for (const file of schemaFiles) {
  const name = file.replace(/\.schema\.json$/, '');
  const schema = JSON.parse(readFileSync(join(protocolDir, file), 'utf8'));
  const typeName = schema.title ?? name;

  const ts = await compile(schema, typeName, { bannerComment: banner });
  writeFileSync(join(outDir, `${name}.ts`), ts);

  const zodCode = jsonSchemaToZod(schema, {
    module: 'esm',
    name: `${typeName}Schema`,
    type: true,
  });
  writeFileSync(join(outDir, `${name}.zod.ts`), `${banner}\n\n${zodCode}`);
}

const indexLines = schemaFiles.flatMap((file) => {
  const name = file.replace(/\.schema\.json$/, '');
  return [`export * from './generated/${name}.js';`, `export * from './generated/${name}.zod.js';`];
});
writeFileSync(join(__dirname, '..', 'src', 'index.ts'), `${banner}\n\n${indexLines.join('\n')}\n`);

console.log(`Generated ${schemaFiles.length} schema(s) -> ${outDir}`);
