import type { KnowledgeRecord } from '@punakawan/schema-types';

/**
 * One entry of `semar_synthesis.open_questions` (see
 * punakawan-go-typescript-detailed-plan.md §8.1, §9.2). Derived from the
 * generated `KnowledgeRecord` type via indexed access rather than
 * hand-retyped, so this stays in sync if the generated schema changes shape.
 */
type OpenQuestions = NonNullable<KnowledgeRecord['semar_synthesis']>['open_questions'];
export type OpenQuestion = NonNullable<OpenQuestions>[number];
