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
import { Textarea } from '@/components/ui/textarea'
import { MultiSelect } from '@/components/multi-select'
import { getGroups } from '../../api'
import { handleBatchEditChannels, parseModelsString } from '../../lib'
import type { BatchEditChannelsParams, BatchModelsMode } from '../../types'
import { ModelMappingEditor } from '../model-mapping-editor'
import { NumericSpinnerInput } from '../numeric-spinner-input'

type BatchEditChannelsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  ids: number[]
  onSuccess: () => void
}

type FieldKey =
  | 'models'
  | 'group'
  | 'priority'
  | 'weight'
  | 'modelMapping'
  | 'paramOverride'
  | 'headerOverride'
  | 'testModel'
  | 'autoBan'

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

const MODELS_MODES: { value: BatchModelsMode; label: string }[] = [
  { value: 'append', label: 'Append models' },
  { value: 'remove', label: 'Remove models' },
  { value: 'replace', label: 'Replace all models' },
]

function isValidJson(value: string): boolean {
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

export function BatchEditChannelsDialog({
  open,
  onOpenChange,
  ids,
  onSuccess,
}: BatchEditChannelsDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [isSaving, setIsSaving] = useState(false)
  const [enabled, setEnabled] = useState<Record<FieldKey, boolean>>({
    models: false,
    group: false,
    priority: false,
    weight: false,
    modelMapping: false,
    paramOverride: false,
    headerOverride: false,
    testModel: false,
    autoBan: false,
  })

  const [modelsMode, setModelsMode] = useState<BatchModelsMode>('append')
  const [models, setModels] = useState('')
  const [groups, setGroups] = useState<string[]>([])
  const [priority, setPriority] = useState(0)
  const [weight, setWeight] = useState(0)
  const [modelMapping, setModelMapping] = useState('')
  const [paramOverride, setParamOverride] = useState('')
  const [headerOverride, setHeaderOverride] = useState('')
  const [testModel, setTestModel] = useState('')
  const [autoBan, setAutoBan] = useState(true)

  const { data: groupsData, isLoading: isLoadingGroups } = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    enabled: open,
  })

  const groupOptions = useMemo(() => {
    const allGroups = new Set([...(groupsData?.data ?? []), ...groups])
    return Array.from(allGroups).map((group) => ({
      value: group,
      label: group,
    }))
  }, [groupsData, groups])

  const setFieldEnabled = (key: FieldKey) => (value: boolean) =>
    setEnabled((prev) => ({ ...prev, [key]: value }))

  const resetForm = () => {
    setEnabled({
      models: false,
      group: false,
      priority: false,
      weight: false,
      modelMapping: false,
      paramOverride: false,
      headerOverride: false,
      testModel: false,
      autoBan: false,
    })
    setModelsMode('append')
    setModels('')
    setGroups([])
    setPriority(0)
    setWeight(0)
    setModelMapping('')
    setParamOverride('')
    setHeaderOverride('')
    setTestModel('')
    setAutoBan(true)
  }

  const handleClose = () => {
    resetForm()
    onOpenChange(false)
  }

  const buildParams = (): BatchEditChannelsParams | null => {
    const params: BatchEditChannelsParams = { ids }

    if (enabled.models) {
      const names = parseModelsString(models.replace(/[\n\r]+/g, ','))
      if (names.length === 0) {
        toast.error(t('Please enter at least one model'))
        return null
      }
      params.models_mode = modelsMode
      params.models = Array.from(new Set(names))
    }

    if (enabled.group) {
      if (groups.length === 0) {
        toast.error(t('Please select at least one group'))
        return null
      }
      params.group = groups.join(',')
    }

    if (enabled.priority) params.priority = priority
    if (enabled.weight) params.weight = weight

    const jsonFields: [
      FieldKey,
      string,
      string,
      'model_mapping' | 'param_override' | 'header_override',
    ][] = [
      ['modelMapping', modelMapping, t('Model Mapping'), 'model_mapping'],
      ['paramOverride', paramOverride, t('Param Override'), 'param_override'],
      [
        'headerOverride',
        headerOverride,
        t('Header Override'),
        'header_override',
      ],
    ]
    for (const [key, value, label, paramKey] of jsonFields) {
      if (!enabled[key]) continue
      const trimmed = value.trim()
      if (trimmed && !isValidJson(trimmed)) {
        toast.error(t('{{field}} must be valid JSON', { field: label }))
        return null
      }
      params[paramKey] = trimmed
    }

    if (enabled.testModel) params.test_model = testModel.trim()
    if (enabled.autoBan) params.auto_ban = autoBan ? 1 : 0

    if (Object.keys(params).length === 1) {
      toast.warning(t('No changes made'))
      return null
    }
    return params
  }

  const handleSave = async () => {
    const params = buildParams()
    if (!params) return

    setIsSaving(true)
    try {
      await handleBatchEditChannels(params, queryClient, () => {
        resetForm()
        onSuccess()
      })
    } finally {
      setIsSaving(false)
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
              {
                count: ids.length,
              }
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-3 py-2'>
          <FieldRow
            id='models'
            label={t('Models')}
            enabled={enabled.models}
            onEnabledChange={setFieldEnabled('models')}
            disabled={isSaving}
          >
            <Select
              items={MODELS_MODES.map((mode) => ({
                value: mode.value,
                label: t(mode.label),
              }))}
              value={modelsMode}
              onValueChange={(v) => v !== null && setModelsMode(v)}
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
            <Textarea
              placeholder={t('Comma or newline separated model names')}
              value={models}
              onChange={(e) => setModels(e.target.value)}
              disabled={isSaving}
              rows={3}
            />
            <p className='text-muted-foreground text-xs'>
              {modelsMode === 'append' &&
                t(
                  'Missing models are added to each channel; existing models are kept.'
                )}
              {modelsMode === 'remove' &&
                t(
                  'These models are removed from each channel. Channels left with no models cause the whole batch to fail.'
                )}
              {modelsMode === 'replace' &&
                t(
                  'Every selected channel will end up with exactly this model list.'
                )}
            </p>
          </FieldRow>

          <FieldRow
            id='group'
            label={t('Groups')}
            enabled={enabled.group}
            onEnabledChange={setFieldEnabled('group')}
            disabled={isSaving}
          >
            {isLoadingGroups ? (
              <Skeleton className='h-10 w-full' />
            ) : (
              <MultiSelect
                options={groupOptions}
                selected={groups}
                onChange={setGroups}
                placeholder={t('Select groups')}
              />
            )}
          </FieldRow>

          <div className='grid gap-3 sm:grid-cols-2'>
            <FieldRow
              id='priority'
              label={t('Priority')}
              enabled={enabled.priority}
              onEnabledChange={setFieldEnabled('priority')}
              disabled={isSaving}
            >
              <NumericSpinnerInput
                value={priority}
                onChange={setPriority}
                min={-999}
                disabled={isSaving}
              />
            </FieldRow>

            <FieldRow
              id='weight'
              label={t('Weight')}
              enabled={enabled.weight}
              onEnabledChange={setFieldEnabled('weight')}
              disabled={isSaving}
            >
              <NumericSpinnerInput
                value={weight}
                onChange={setWeight}
                min={0}
                disabled={isSaving}
              />
            </FieldRow>
          </div>

          <FieldRow
            id='modelMapping'
            label={t('Model Mapping')}
            enabled={enabled.modelMapping}
            onEnabledChange={setFieldEnabled('modelMapping')}
            disabled={isSaving}
          >
            <ModelMappingEditor
              value={modelMapping}
              onChange={setModelMapping}
              disabled={isSaving}
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Overwrites the mapping of every selected channel. Leave empty to clear it.'
              )}
            </p>
          </FieldRow>

          <FieldRow
            id='paramOverride'
            label={t('Param Override')}
            enabled={enabled.paramOverride}
            onEnabledChange={setFieldEnabled('paramOverride')}
            disabled={isSaving}
          >
            <Textarea
              placeholder='{"temperature": 0.7}'
              value={paramOverride}
              onChange={(e) => setParamOverride(e.target.value)}
              disabled={isSaving}
              rows={4}
              className='font-mono text-xs'
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Overwrites the override of every selected channel. Leave empty to clear it.'
              )}
            </p>
          </FieldRow>

          <FieldRow
            id='headerOverride'
            label={t('Header Override')}
            enabled={enabled.headerOverride}
            onEnabledChange={setFieldEnabled('headerOverride')}
            disabled={isSaving}
          >
            <Textarea
              placeholder='{"X-Custom-Header": "value"}'
              value={headerOverride}
              onChange={(e) => setHeaderOverride(e.target.value)}
              disabled={isSaving}
              rows={4}
              className='font-mono text-xs'
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Overwrites the override of every selected channel. Leave empty to clear it.'
              )}
            </p>
          </FieldRow>

          <FieldRow
            id='testModel'
            label={t('Test Model')}
            enabled={enabled.testModel}
            onEnabledChange={setFieldEnabled('testModel')}
            disabled={isSaving}
          >
            <Input
              placeholder={t('Leave empty to clear the test model')}
              value={testModel}
              onChange={(e) => setTestModel(e.target.value)}
              disabled={isSaving}
            />
          </FieldRow>

          <FieldRow
            id='autoBan'
            label={t('Auto Ban')}
            enabled={enabled.autoBan}
            onEnabledChange={setFieldEnabled('autoBan')}
            disabled={isSaving}
          >
            <div className='flex items-center gap-2'>
              <Switch
                checked={autoBan}
                onCheckedChange={setAutoBan}
                disabled={isSaving}
              />
              <span className='text-muted-foreground text-sm'>
                {autoBan ? t('Enabled') : t('Disabled')}
              </span>
            </div>
          </FieldRow>
        </div>

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
