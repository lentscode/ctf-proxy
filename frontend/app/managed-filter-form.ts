import { z } from 'zod'
import { parse, stringify } from 'yaml'

export const filterProtocolSchema = z.enum(['tcp', 'http'])
export const filterDirectionSchema = z.enum(['request', 'response'])
export const matchFieldSchema = z.enum(['tcp.body', 'http.path', 'http.header', 'http.body'])
export const matchOperatorSchema = z.enum(['exact', 'contains', 'not_contains', 'prefix', 'suffix', 'regex'])

export type FilterProtocol = z.infer<typeof filterProtocolSchema>
export type FilterDirection = z.infer<typeof filterDirectionSchema>
export type MatchField = z.infer<typeof matchFieldSchema>
export type MatchOperator = z.infer<typeof matchOperatorSchema>

const conditionSchema = z.object({
  field: matchFieldSchema,
  header: z.string(),
  operator: matchOperatorSchema,
  value: z.string(),
})

export const managedFilterDraftSchema = z.object({
  name: z.string().trim().min(1, 'Filter name is required.'),
  active: z.boolean(),
  protocol: filterProtocolSchema,
  direction: filterDirectionSchema,
  conditions: z.array(conditionSchema).min(1, 'Add at least one condition.'),
}).superRefine((draft, context) => {
  for (const [index, condition] of draft.conditions.entries()) {
    if (!isFieldAvailable(condition.field, draft.protocol, draft.direction)) {
      context.addIssue({
        code: 'custom',
        path: ['conditions', index, 'field'],
        message: 'This field is not available for the selected protocol and direction.',
      })
    }
    if (condition.field === 'http.header' && !condition.header.trim()) {
      context.addIssue({
        code: 'custom',
        path: ['conditions', index, 'header'],
        message: 'Header name is required for HTTP header matching.',
      })
    }
    if (condition.field !== 'http.header' && condition.header) {
      context.addIssue({
        code: 'custom',
        path: ['conditions', index, 'header'],
        message: 'Header name is only valid for HTTP header matching.',
      })
    }
  }
})

export type ManagedFilterDraft = z.infer<typeof managedFilterDraftSchema>

const yamlBooleanSchema = z.preprocess(
  (value) => value === 'true' ? true : value === 'false' ? false : value,
  z.boolean(),
)

const yamlDocumentSchema = z.object({
  version: z.union([z.literal(1), z.literal('1')]),
  filters: z.array(z.object({
    name: z.string(),
    active: yamlBooleanSchema.optional().default(false),
    protocol: filterProtocolSchema,
    direction: filterDirectionSchema,
    action: z.literal('reject'),
    match: z.object({
      all: z.array(z.object({
        field: matchFieldSchema,
        header: z.string().optional().default(''),
        operator: matchOperatorSchema,
        value: z.string(),
      })).min(1),
    }),
  })).length(1),
})

const fieldLabels: Record<MatchField, string> = {
  'tcp.body': 'TCP body',
  'http.path': 'HTTP path',
  'http.header': 'HTTP header',
  'http.body': 'HTTP body',
}

// availableFields returns the supported match fields for a rule's protocol and direction.
export function availableFields(protocol: FilterProtocol, direction: FilterDirection): MatchField[] {
  if (protocol === 'tcp') return ['tcp.body']
  return direction === 'request'
    ? ['http.path', 'http.header', 'http.body']
    : ['http.header', 'http.body']
}

// isFieldAvailable reports whether the filter compiler accepts a field in this context.
export function isFieldAvailable(field: MatchField, protocol: FilterProtocol, direction: FilterDirection): boolean {
  return availableFields(protocol, direction).includes(field)
}

// labelForField supplies a concise operator-facing field label.
export function labelForField(field: MatchField): string {
  return fieldLabels[field]
}

// createEmptyDraft creates a valid initial draft scoped to a target proxy protocol.
export function createEmptyDraft(protocol: FilterProtocol): ManagedFilterDraft {
  return {
    name: '', active: false, protocol, direction: 'request',
    conditions: [{ field: availableFields(protocol, 'request')[0], header: '', operator: 'contains', value: '' }],
  }
}

// parseManagedFilterYAML converts the server's single-rule document into form state.
export function parseManagedFilterYAML(source: string): ManagedFilterDraft {
  const document = yamlDocumentSchema.parse(parse(source, { schema: 'failsafe' }))
  const rule = document.filters[0]
  return managedFilterDraftSchema.parse({
    name: rule.name,
    active: rule.active,
    protocol: rule.protocol,
    direction: rule.direction,
    conditions: rule.match.all.map((condition) => ({
      field: condition.field,
      header: condition.header,
      operator: condition.operator,
      value: condition.value,
    })),
  })
}

// serializeManagedFilterYAML creates the canonical single-rule request accepted by the API.
export function serializeManagedFilterYAML(draft: ManagedFilterDraft): string {
  const valid = managedFilterDraftSchema.parse(draft)
  return stringify({
    version: 1,
    filters: [{
      name: valid.name,
      active: valid.active,
      protocol: valid.protocol,
      direction: valid.direction,
      action: 'reject',
      match: { all: valid.conditions.map(({ field, header, operator, value }) => ({
        field,
        ...(field === 'http.header' ? { header } : {}),
        operator,
        value,
      })) },
    }],
  }, { defaultStringType: 'QUOTE_DOUBLE', lineWidth: 0 })
}
