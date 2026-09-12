import createClient from 'openapi-fetch'
import type { paths } from './generated/schema.ts'

export type GeneratedApiClient = ReturnType<typeof createGeneratedApiClient>

export function createGeneratedApiClient(
  options: Parameters<typeof createClient<paths>>[0],
) {
  return createClient<paths>(options)
}
