/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { MODEL_FETCHABLE_TYPES } from '../constants'
import type {
  BatchEditChannelsParams,
  BatchEditFieldKey,
  BatchModelsMode,
} from '../types'
import { parseModelsString } from './model-mapping-validation'

// NOTE: Keep in sync with batchEditFieldTypes in model/channel_batch_edit.go.
const BATCH_EDIT_FIELD_TYPES: Partial<
  Record<BatchEditFieldKey, ReadonlySet<number>>
> = {
  forceFormat: new Set([1]),
  codexDisguise: new Set([1]),
  claudeDisguiseMode: new Set([14, 59]),
  claudeBetaQuery: new Set([14, 59]),
  autoCacheControl: new Set([1, 14, 58, 59]),
  upstreamCheck: MODEL_FETCHABLE_TYPES,
  upstreamAutoSync: MODEL_FETCHABLE_TYPES,
  upstreamIgnored: MODEL_FETCHABLE_TYPES,
}

/**
 * A field is applicable when it has no type restriction, or every selected
 * channel type is allowed.
 */
export function isBatchFieldApplicable(
  key: BatchEditFieldKey,
  channelTypes: number[]
): boolean {
  const allowed = BATCH_EDIT_FIELD_TYPES[key]
  if (!allowed) return true
  return channelTypes.length > 0 && channelTypes.every((t) => allowed.has(t))
}

export type BatchEditValues = {
  modelsMode: BatchModelsMode
  models: string
  groups: string[]
  priority: number
  weight: number
  modelMapping: string
  paramOverride: string
  headerOverride: string
  testModel: string
  autoBan: boolean
  autoBanOptimistic: boolean
  autoUnbanByBalance: boolean
  statusCodeMapping: string
  retryOn429: number
  proxy: string
  systemPrompt: string
  systemPromptOverride: boolean
  thinkingToContent: boolean
  passThroughBody: boolean
  forceFormat: boolean
  codexDisguise: boolean
  claudeDisguiseMode: number
  claudeBetaQuery: boolean
  autoCacheControl: boolean
  upstreamCheck: boolean
  upstreamAutoSync: boolean
  ignoredMode: BatchModelsMode
  ignored: string
}

export const DEFAULT_BATCH_EDIT_VALUES: BatchEditValues = {
  modelsMode: 'append',
  models: '',
  groups: [],
  priority: 0,
  weight: 0,
  modelMapping: '',
  paramOverride: '',
  headerOverride: '',
  testModel: '',
  autoBan: true,
  autoBanOptimistic: false,
  autoUnbanByBalance: true,
  statusCodeMapping: '',
  retryOn429: 0,
  proxy: '',
  systemPrompt: '',
  systemPromptOverride: false,
  thinkingToContent: false,
  passThroughBody: false,
  forceFormat: false,
  codexDisguise: false,
  claudeDisguiseMode: 0,
  claudeBetaQuery: false,
  autoCacheControl: false,
  upstreamCheck: false,
  upstreamAutoSync: false,
  ignoredMode: 'append',
  ignored: '',
}

export type BatchEditEnabled = Record<BatchEditFieldKey, boolean>

export const ALL_BATCH_FIELDS_OFF: BatchEditEnabled = {
  models: false,
  group: false,
  priority: false,
  weight: false,
  modelMapping: false,
  paramOverride: false,
  headerOverride: false,
  testModel: false,
  autoBan: false,
  autoBanOptimistic: false,
  autoUnbanByBalance: false,
  statusCodeMapping: false,
  retryOn429: false,
  proxy: false,
  systemPrompt: false,
  systemPromptOverride: false,
  thinkingToContent: false,
  passThroughBody: false,
  forceFormat: false,
  codexDisguise: false,
  claudeDisguiseMode: false,
  claudeBetaQuery: false,
  autoCacheControl: false,
  upstreamCheck: false,
  upstreamAutoSync: false,
  upstreamIgnored: false,
}

function isValidJson(value: string): boolean {
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

/**
 * Same rule as the channel form: a JSON object whose keys and values are
 * HTTP status codes between 100 and 599.
 */
export function isValidStatusCodeMapping(value: string): boolean {
  const trimmed = value.trim()
  if (!trimmed) return true
  try {
    const parsed: unknown = JSON.parse(trimmed)
    if (
      typeof parsed !== 'object' ||
      parsed === null ||
      Array.isArray(parsed)
    ) {
      return false
    }
    return Object.entries(parsed).every(([from, to]) => {
      const fromCode = Number(from)
      const toCode = Number(to)
      return (
        Number.isInteger(fromCode) &&
        Number.isInteger(toCode) &&
        fromCode >= 100 &&
        fromCode <= 599 &&
        toCode >= 100 &&
        toCode <= 599
      )
    })
  } catch {
    return false
  }
}

function splitNames(raw: string): string[] {
  return Array.from(new Set(parseModelsString(raw.replace(/[\n\r]+/g, ','))))
}

export type BatchEditBuildResult =
  | { params: BatchEditChannelsParams }
  | { error: string; field?: BatchEditFieldKey }

type Translate = (key: string, options?: Record<string, unknown>) => string

/**
 * Build the request body from the checked fields. Only fields that are both
 * checked and applicable to every selected channel type are sent.
 */
export function buildBatchEditParams(
  ids: number[],
  enabled: BatchEditEnabled,
  values: BatchEditValues,
  channelTypes: number[],
  t: Translate
): BatchEditBuildResult {
  const on = (key: BatchEditFieldKey) =>
    enabled[key] && isBatchFieldApplicable(key, channelTypes)

  const params: BatchEditChannelsParams = { ids }

  if (on('models')) {
    const names = splitNames(values.models)
    if (names.length === 0) {
      return { error: t('Please enter at least one model'), field: 'models' }
    }
    params.models_mode = values.modelsMode
    params.models = names
  }

  if (on('group')) {
    if (values.groups.length === 0) {
      return { error: t('Please select at least one group'), field: 'group' }
    }
    params.group = values.groups.join(',')
  }

  if (on('priority')) params.priority = values.priority
  if (on('weight')) params.weight = values.weight

  const jsonFields = [
    ['modelMapping', values.modelMapping, 'Model Mapping', 'model_mapping'],
    ['paramOverride', values.paramOverride, 'Param Override', 'param_override'],
    [
      'headerOverride',
      values.headerOverride,
      'Header Override',
      'header_override',
    ],
  ] as const
  for (const [key, value, label, paramKey] of jsonFields) {
    if (!on(key)) continue
    const trimmed = value.trim()
    if (trimmed && !isValidJson(trimmed)) {
      return {
        error: t('{{field}} must be valid JSON', { field: t(label) }),
        field: key,
      }
    }
    params[paramKey] = trimmed
  }

  if (on('testModel')) params.test_model = values.testModel.trim()
  if (on('autoBan')) params.auto_ban = values.autoBan ? 1 : 0
  if (on('autoBanOptimistic')) {
    params.auto_ban_optimistic = values.autoBanOptimistic ? 1 : 0
  }
  if (on('autoUnbanByBalance')) {
    params.auto_unban_by_balance = values.autoUnbanByBalance ? 1 : 0
  }

  if (on('statusCodeMapping')) {
    if (!isValidStatusCodeMapping(values.statusCodeMapping)) {
      return {
        error: t('Status code mapping must use valid HTTP status codes'),
        field: 'statusCodeMapping',
      }
    }
    params.status_code_mapping = values.statusCodeMapping.trim()
  }

  if (on('retryOn429')) params.retry_on_429 = values.retryOn429
  if (on('proxy')) params.proxy = values.proxy.trim()
  if (on('systemPrompt')) params.system_prompt = values.systemPrompt
  if (on('systemPromptOverride')) {
    params.system_prompt_override = values.systemPromptOverride
  }
  if (on('thinkingToContent')) {
    params.thinking_to_content = values.thinkingToContent
  }
  if (on('passThroughBody')) {
    params.pass_through_body_enabled = values.passThroughBody
  }
  if (on('forceFormat')) params.force_format = values.forceFormat
  if (on('codexDisguise')) params.codex_disguise = values.codexDisguise
  if (on('claudeDisguiseMode')) {
    params.claude_code_disguise_mode = values.claudeDisguiseMode
  }
  if (on('claudeBetaQuery')) params.claude_beta_query = values.claudeBetaQuery
  if (on('autoCacheControl')) {
    params.auto_cache_control = values.autoCacheControl
  }
  if (on('upstreamCheck')) {
    params.upstream_model_update_check_enabled = values.upstreamCheck
  }
  if (on('upstreamAutoSync')) {
    params.upstream_model_update_auto_sync_enabled = values.upstreamAutoSync
  }

  if (on('upstreamIgnored')) {
    const names = splitNames(values.ignored)
    if (names.length === 0 && values.ignoredMode !== 'replace') {
      return {
        error: t('Please enter at least one model'),
        field: 'upstreamIgnored',
      }
    }
    params.upstream_model_update_ignored_models_mode = values.ignoredMode
    params.upstream_model_update_ignored_models = names
  }

  if (Object.keys(params).length === 1) {
    return { error: t('No changes made') }
  }
  return { params }
}
