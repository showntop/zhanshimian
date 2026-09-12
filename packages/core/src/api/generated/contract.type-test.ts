import type { components, paths } from './schema.ts'

type Expect<T extends true> = T
type HasAssessment = Expect<'/v1/assessments' extends keyof paths ? true : false>
type HasOperation = Expect<
  '/v1/operations/{id}' extends keyof paths ? true : false
>
type HasPlanSet = Expect<
  '/v1/plan-sets/{id}' extends keyof paths ? true : false
>
type HasExecution = Expect<
  '/v1/executions/{id}' extends keyof paths ? true : false
>
type SourceKind = components['schemas']['DisplayMedia']['source_kind']
type AllowedSourceKinds =
  | 'user_original'
  | 'generated_preview'
  | 'bundled_reference'
  | 'demo_example'
type ExactSourceKinds = Expect<
  Exclude<SourceKind, AllowedSourceKinds> extends never
    ? Exclude<AllowedSourceKinds, SourceKind> extends never
      ? true
      : false
    : false
>

export type ContractAssertions = [
  HasAssessment,
  HasOperation,
  HasPlanSet,
  HasExecution,
  ExactSourceKinds,
]
