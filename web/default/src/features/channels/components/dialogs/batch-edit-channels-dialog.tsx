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
import { useMemo, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { MultiSelect } from '@/components/multi-select'
import { getGroups } from '../../api'
import {
  ALL_BATCH_FIELDS_OFF,
  DEFAULT_BATCH_EDIT_VALUES,
  buildBatchEditParams,
  handleBatchEditChannels,
  isBatchFieldApplicable,
  type BatchEditEnabled,
  type BatchEditValues,
} from '../../lib'
import {
  CLAUDE_DISGUISE_HEADER,
  CLAUDE_DISGUISE_SYSTEM_PROMPT,
  CLAUDE_DISGUISE_UA,
  type BatchEditFieldKey,
  type BatchModelsMode,
} from '../../types'
import { ModelMappingEditor } from '../model-mapping-editor'
import { NumericSpinnerInput } from '../numeric-spinner-input'

type BatchEditChannelsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  ids: number[]
  channelTypes: number[]
  onSuccess: () => void
}

type TabKey = 'basic' | 'routing' | 'request' | 'specific'

const TAB_FIELDS: Record<TabKey, BatchEditFieldKey[]> = {
  basic: [
    'testModel',
    'autoBan',
    'autoBanOptimistic',
    'autoUnbanByBalance',
    'statusCodeMapping',
    'retryOn429',
  ],
  routing: ['models', 'group', 'priority', 'weight', 'modelMapping'],
  request: [
    'paramOverride',
    'headerOverride',
    'proxy',
    'systemPrompt',
    'systemPromptOverride',
    'thinkingToContent',
    'passThroughBody',
  ],
  specific: [
    'forceFormat',
    'codexDisguise',
    'claudeDisguiseMode',
    'claudeBetaQuery',
    'autoCacheControl',
    'upstreamCheck',
    'upstreamAutoSync',
    'upstreamIgnored',
  ],
}

const TAB_ORDER: TabKey[] = ['basic', 'routing', 'request', 'specific']

const TAB_LABELS: Record<TabKey, string> = {
  basic: 'Basic',
  routing: 'Routing & Models',
  request: 'Request Handling',
  specific: 'Type Specific',
}

const MODELS_MODES: { value: BatchModelsMode; label: string }[] = [
  { value: 'append', label: 'Append models' },
  { value: 'remove', label: 'Remove models' },
  { value: 'replace', label: 'Replace all models' },
]

const CLAUDE_DISGUISE_BITS = [
  { bit: CLAUDE_DISGUISE_UA, label: 'User-Agent disguise' },
  {
    bit: CLAUDE_DISGUISE_HEADER,
    label: 'Header disguise (X-App, anthropic-beta)',
  },
  {
    bit: CLAUDE_DISGUISE_SYSTEM_PROMPT,
    label: 'System prompt disguise (prompt injection + metadata)',
  },
]

type FieldRowProps = {
  id: string
  label: string
  enabled: boolean
  onEnabledChange: (enabled: boolean) => void
  disabled: boolean
  children: ReactNode
}

function FieldRow(props: FieldRowProps) {
  return (
    <div className='space-y-2 rounded-md border p-3'>
      <div className='flex items-center gap-2'>
        <Checkbox
          id={`batch-edit-${props.id}`}
          checked={props.enabled}
          onCheckedChange={(value) => props.onEnabledChange(!!value)}
          disabled={props.disabled}
        />
        <Label
          htmlFor={`batch-edit-${props.id}`}
          className='cursor-pointer font-medium'
        >
          {props.label}
        </Label>
      </div>
      {props.enabled && <div className='space-y-2'>{props.children}</div>}
    </div>
  )
}

function SwitchValue(props: {
  checked: boolean
  onChange: (checked: boolean) => void
  disabled: boolean
}) {
  const { t } = useTranslation()
  return (
    <div className='flex items-center gap-2'>
      <Switch
        checked={props.checked}
        onCheckedChange={props.onChange}
        disabled={props.disabled}
      />
      <span className='text-muted-foreground text-sm'>
        {props.checked ? t('Enabled') : t('Disabled')}
      </span>
    </div>
  )
}

function ModeSelect(props: {
  value: BatchModelsMode
  onChange: (mode: BatchModelsMode) => void
}) {
  const { t } = useTranslation()
  return (
    <Select
      items={MODELS_MODES.map((mode) => ({
        value: mode.value,
        label: t(mode.label),
      }))}
      value={props.value}
      onValueChange={(v) => v !== null && props.onChange(v)}
    >
      <SelectTrigger className='w-56'>
        <SelectValue />
      </SelectTrigger>
      <SelectContent alignItemWithTrigger={false}>
        <SelectGroup>
          {MODELS_MODES.map((mode) => (
            <SelectItem key={mode.value} value={mode.value}>
              {t(mode.label)}
            </SelectItem>
          ))}
        </SelectGroup>
      </SelectContent>
    </Select>
  )
}

export function BatchEditChannelsDialog({
  open,
  onOpenChange,
  ids,
  channelTypes,
  onSuccess,
}: BatchEditChannelsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [isSaving, setIsSaving] = useState(false)
  const [activeTab, setActiveTab] = useState<TabKey>('basic')
  const [enabled, setEnabled] = useState<BatchEditEnabled>(ALL_BATCH_FIELDS_OFF)
  const [values, setValues] = useState<BatchEditValues>(
    DEFAULT_BATCH_EDIT_VALUES
  )

  const { data: groupsData, isLoading: isLoadingGroups } = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    enabled: open,
  })

  const groupOptions = useMemo(() => {
    const allGroups = new Set([...(groupsData?.data ?? []), ...values.groups])
    return Array.from(allGroups).map((group) => ({
      value: group,
      label: group,
    }))
  }, [groupsData, values.groups])

  const visibleFields = useMemo(() => {
    const visible = new Set<BatchEditFieldKey>()
    for (const keys of Object.values(TAB_FIELDS)) {
      for (const key of keys) {
        if (isBatchFieldApplicable(key, channelTypes)) visible.add(key)
      }
    }
    return visible
  }, [channelTypes])

  const visibleTabs = TAB_ORDER.filter((tab) =>
    TAB_FIELDS[tab].some((key) => visibleFields.has(key))
  )
  const currentTab = visibleTabs.includes(activeTab)
    ? activeTab
    : (visibleTabs[0] ?? 'basic')

  const checkedCount = (tab: TabKey) =>
    TAB_FIELDS[tab].filter((key) => visibleFields.has(key) && enabled[key])
      .length

  const setValue = <K extends keyof BatchEditValues>(
    key: K,
    value: BatchEditValues[K]
  ) => setValues((prev) => ({ ...prev, [key]: value }))

  const resetForm = () => {
    setEnabled(ALL_BATCH_FIELDS_OFF)
    setValues(DEFAULT_BATCH_EDIT_VALUES)
    setActiveTab('basic')
  }

  const handleClose = () => {
    resetForm()
    onOpenChange(false)
  }

  const handleSave = async () => {
    const result = buildBatchEditParams(ids, enabled, values, channelTypes, t)
    if ('error' in result) {
      toast.error(result.error)
      return
    }

    setIsSaving(true)
    try {
      await handleBatchEditChannels(result.params, queryClient, () => {
        resetForm()
        onSuccess()
      })
    } finally {
      setIsSaving(false)
    }
  }

  const row = (
    key: BatchEditFieldKey,
    label: string,
    children: ReactNode
  ): ReactNode =>
    visibleFields.has(key) ? (
      <FieldRow
        key={key}
        id={key}
        label={label}
        enabled={enabled[key]}
        onEnabledChange={(value) =>
          setEnabled((prev) => ({ ...prev, [key]: value }))
        }
        disabled={isSaving}
      >
        {children}
      </FieldRow>
    ) : null

  const hint = (text: string) => (
    <p className='text-muted-foreground text-xs'>{text}</p>
  )

  const renderTab = (tab: TabKey): ReactNode => {
    switch (tab) {
      case 'basic':
        return (
          <>
            {row(
              'testModel',
              t('Test Model'),
              <Input
                placeholder={t('Leave empty to clear the test model')}
                value={values.testModel}
                onChange={(e) => setValue('testModel', e.target.value)}
                disabled={isSaving}
              />
            )}
            {row(
              'autoBan',
              t('Auto Ban'),
              <SwitchValue
                checked={values.autoBan}
                onChange={(v) => setValue('autoBan', v)}
                disabled={isSaving}
              />
            )}
            {row(
              'autoBanOptimistic',
              t('Auto Unban (Optimistic)'),
              <SwitchValue
                checked={values.autoBanOptimistic}
                onChange={(v) => setValue('autoBanOptimistic', v)}
                disabled={isSaving}
              />
            )}
            {row(
              'autoUnbanByBalance',
              t('Auto Unban on Balance'),
              <SwitchValue
                checked={values.autoUnbanByBalance}
                onChange={(v) => setValue('autoUnbanByBalance', v)}
                disabled={isSaving}
              />
            )}
            {row(
              'statusCodeMapping',
              t('Status Code Mapping'),
              <>
                <Textarea
                  placeholder='{"400": "500"}'
                  value={values.statusCodeMapping}
                  onChange={(e) =>
                    setValue('statusCodeMapping', e.target.value)
                  }
                  disabled={isSaving}
                  rows={3}
                  className='font-mono text-xs'
                />
                {hint(
                  t(
                    'Overwrites the mapping of every selected channel. Leave empty to clear it.'
                  )
                )}
              </>
            )}
            {row(
              'retryOn429',
              t('Retry on 429'),
              <NumericSpinnerInput
                value={values.retryOn429}
                onChange={(v) => setValue('retryOn429', v)}
                min={0}
                max={50}
                disabled={isSaving}
              />
            )}
          </>
        )
      case 'routing':
        return (
          <>
            {row(
              'models',
              t('Models'),
              <>
                <ModeSelect
                  value={values.modelsMode}
                  onChange={(mode) => setValue('modelsMode', mode)}
                />
                <Textarea
                  placeholder={t('Comma or newline separated model names')}
                  value={values.models}
                  onChange={(e) => setValue('models', e.target.value)}
                  disabled={isSaving}
                  rows={3}
                />
                {values.modelsMode === 'append' &&
                  hint(
                    t(
                      'Missing models are added to each channel; existing models are kept.'
                    )
                  )}
                {values.modelsMode === 'remove' &&
                  hint(
                    t(
                      'These models are removed from each channel. Channels left with no models cause the whole batch to fail.'
                    )
                  )}
                {values.modelsMode === 'replace' &&
                  hint(
                    t(
                      'Every selected channel will end up with exactly this model list.'
                    )
                  )}
              </>
            )}
            {row(
              'group',
              t('Groups'),
              isLoadingGroups ? (
                <Skeleton className='h-10 w-full' />
              ) : (
                <MultiSelect
                  options={groupOptions}
                  selected={values.groups}
                  onChange={(groups) => setValue('groups', groups)}
                  placeholder={t('Select groups')}
                />
              )
            )}
            <div className='grid gap-3 sm:grid-cols-2'>
              {row(
                'priority',
                t('Priority'),
                <NumericSpinnerInput
                  value={values.priority}
                  onChange={(v) => setValue('priority', v)}
                  min={-999}
                  disabled={isSaving}
                />
              )}
              {row(
                'weight',
                t('Weight'),
                <NumericSpinnerInput
                  value={values.weight}
                  onChange={(v) => setValue('weight', v)}
                  min={0}
                  disabled={isSaving}
                />
              )}
            </div>
            {row(
              'modelMapping',
              t('Model Mapping'),
              <>
                <ModelMappingEditor
                  value={values.modelMapping}
                  onChange={(v) => setValue('modelMapping', v)}
                  disabled={isSaving}
                />
                {hint(
                  t(
                    'Overwrites the mapping of every selected channel. Leave empty to clear it.'
                  )
                )}
              </>
            )}
          </>
        )
      case 'request':
        return (
          <>
            {row(
              'paramOverride',
              t('Param Override'),
              <>
                <Textarea
                  placeholder='{"temperature": 0.7}'
                  value={values.paramOverride}
                  onChange={(e) => setValue('paramOverride', e.target.value)}
                  disabled={isSaving}
                  rows={4}
                  className='font-mono text-xs'
                />
                {hint(
                  t(
                    'Overwrites the override of every selected channel. Leave empty to clear it.'
                  )
                )}
              </>
            )}
            {row(
              'headerOverride',
              t('Header Override'),
              <>
                <Textarea
                  placeholder='{"X-Custom-Header": "value"}'
                  value={values.headerOverride}
                  onChange={(e) => setValue('headerOverride', e.target.value)}
                  disabled={isSaving}
                  rows={4}
                  className='font-mono text-xs'
                />
                {hint(
                  t(
                    'Overwrites the override of every selected channel. Leave empty to clear it.'
                  )
                )}
              </>
            )}
            {row(
              'proxy',
              t('Proxy Address'),
              <Input
                placeholder='http://127.0.0.1:7890'
                value={values.proxy}
                onChange={(e) => setValue('proxy', e.target.value)}
                disabled={isSaving}
              />
            )}
            {row(
              'systemPrompt',
              t('System Prompt'),
              <Textarea
                value={values.systemPrompt}
                onChange={(e) => setValue('systemPrompt', e.target.value)}
                disabled={isSaving}
                rows={4}
              />
            )}
            {row(
              'systemPromptOverride',
              t('System Prompt Concatenation'),
              <SwitchValue
                checked={values.systemPromptOverride}
                onChange={(v) => setValue('systemPromptOverride', v)}
                disabled={isSaving}
              />
            )}
            {row(
              'thinkingToContent',
              t('Thinking to Content'),
              <SwitchValue
                checked={values.thinkingToContent}
                onChange={(v) => setValue('thinkingToContent', v)}
                disabled={isSaving}
              />
            )}
            {row(
              'passThroughBody',
              t('Pass Through Body'),
              <SwitchValue
                checked={values.passThroughBody}
                onChange={(v) => setValue('passThroughBody', v)}
                disabled={isSaving}
              />
            )}
          </>
        )
      case 'specific':
        return (
          <>
            {row(
              'forceFormat',
              t('Force Format'),
              <SwitchValue
                checked={values.forceFormat}
                onChange={(v) => setValue('forceFormat', v)}
                disabled={isSaving}
              />
            )}
            {row(
              'codexDisguise',
              t('Codex disguise'),
              <SwitchValue
                checked={values.codexDisguise}
                onChange={(v) => setValue('codexDisguise', v)}
                disabled={isSaving}
              />
            )}
            {row(
              'claudeDisguiseMode',
              t('Claude Code disguise'),
              <div className='space-y-2'>
                {CLAUDE_DISGUISE_BITS.map(({ bit, label }) => (
                  <div
                    key={bit}
                    className='flex items-center justify-between gap-3'
                  >
                    <span className='text-sm'>{t(label)}</span>
                    <Switch
                      checked={(values.claudeDisguiseMode & bit) !== 0}
                      onCheckedChange={(checked) =>
                        setValue(
                          'claudeDisguiseMode',
                          checked
                            ? values.claudeDisguiseMode | bit
                            : values.claudeDisguiseMode & ~bit
                        )
                      }
                      disabled={isSaving}
                    />
                  </div>
                ))}
              </div>
            )}
            {row(
              'claudeBetaQuery',
              t('Allow Claude beta query passthrough'),
              <SwitchValue
                checked={values.claudeBetaQuery}
                onChange={(v) => setValue('claudeBetaQuery', v)}
                disabled={isSaving}
              />
            )}
            {row(
              'autoCacheControl',
              t('Auto prompt cache control'),
              <SwitchValue
                checked={values.autoCacheControl}
                onChange={(v) => setValue('autoCacheControl', v)}
                disabled={isSaving}
              />
            )}
            {row(
              'upstreamCheck',
              t('Upstream Model Update Check'),
              <>
                <SwitchValue
                  checked={values.upstreamCheck}
                  onChange={(v) => setValue('upstreamCheck', v)}
                  disabled={isSaving}
                />
                {hint(
                  t(
                    'Turning the check off also turns off auto sync and clears detected models.'
                  )
                )}
              </>
            )}
            {row(
              'upstreamAutoSync',
              t('Auto Sync Upstream Models'),
              <>
                <SwitchValue
                  checked={values.upstreamAutoSync}
                  onChange={(v) => setValue('upstreamAutoSync', v)}
                  disabled={isSaving}
                />
                {hint(
                  t(
                    'Auto sync only takes effect when the upstream model update check is on.'
                  )
                )}
              </>
            )}
            {row(
              'upstreamIgnored',
              t('Ignored upstream models'),
              <>
                <ModeSelect
                  value={values.ignoredMode}
                  onChange={(mode) => setValue('ignoredMode', mode)}
                />
                <Textarea
                  placeholder={t('Comma or newline separated model names')}
                  value={values.ignored}
                  onChange={(e) => setValue('ignored', e.target.value)}
                  disabled={isSaving}
                  rows={3}
                />
                {values.ignoredMode === 'replace' &&
                  hint(t('Leave empty to clear the ignored list.'))}
              </>
            )}
          </>
        )
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className='max-h-[90vh] max-w-2xl overflow-y-auto'>
        <DialogHeader>
          <DialogTitle>{t('Batch Edit Channels')}</DialogTitle>
          <DialogDescription>
            {t(
              'Only checked fields will be changed for the {{count}} selected channel(s).',
              { count: ids.length }
            )}
          </DialogDescription>
        </DialogHeader>

        <Tabs
          value={currentTab}
          onValueChange={(value) => setActiveTab(value as TabKey)}
        >
          <TabsList className='w-full'>
            {visibleTabs.map((tab) => {
              const count = checkedCount(tab)
              return (
                <TabsTrigger key={tab} value={tab}>
                  {t(TAB_LABELS[tab])}
                  {count > 0 && (
                    <Badge variant='secondary' className='ml-1 px-1.5'>
                      {count}
                    </Badge>
                  )}
                </TabsTrigger>
              )
            })}
          </TabsList>
          {visibleTabs.map((tab) => (
            <TabsContent key={tab} value={tab} className='space-y-3 py-2'>
              {renderTab(tab)}
            </TabsContent>
          ))}
        </Tabs>

        <DialogFooter>
          <Button variant='outline' onClick={handleClose} disabled={isSaving}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleSave} disabled={isSaving}>
            {isSaving && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {isSaving ? t('Saving...') : t('Save Changes')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
