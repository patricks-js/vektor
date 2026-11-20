import type { Resource } from '../../lib/api/client'

export function metadata(resource: Resource): Record<string, unknown> {
  try {
    const value: unknown = JSON.parse(resource.metadata_json)
    return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
  } catch {
    return {}
  }
}

export function metadataText(resource: Resource, key: string) {
  const value = metadata(resource)[key]
  return typeof value === 'string' && value ? value : undefined
}

export function displayName(resource: Resource) {
  return resource.display_name || resource.id
}
