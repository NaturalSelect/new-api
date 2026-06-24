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
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

const poeLogSchema = z.object({
  poe_log_setting: z.object({
    enabled: z.boolean(),
    sync_interval: z.coerce
      .number()
      .int()
      .min(1, 'Interval must be at least 1 second'),
    key_deduplicate: z.boolean(),
    free_models: z.string(),
    sync_to_consume_log: z.boolean(),
  }),
})

type PoeLogFormValues = z.output<typeof poeLogSchema>
type PoeLogFormInput = z.input<typeof poeLogSchema>

type PoeLogSettingsSectionProps = {
  defaultValues: {
    'poe_log_setting.enabled': boolean
    'poe_log_setting.sync_interval': number
    'poe_log_setting.key_deduplicate': boolean
    'poe_log_setting.free_models': string[] | string
    'poe_log_setting.sync_to_consume_log': boolean
  }
}

type NormalizedPoeLogValues = {
  'poe_log_setting.enabled': boolean
  'poe_log_setting.sync_interval': number
  'poe_log_setting.key_deduplicate': boolean
  'poe_log_setting.free_models': string[]
  'poe_log_setting.sync_to_consume_log': boolean
}

function areNormalizedValuesEqual(
  value: NormalizedPoeLogValues[keyof NormalizedPoeLogValues],
  baseline: NormalizedPoeLogValues[keyof NormalizedPoeLogValues]
): boolean {
  if (Array.isArray(value) && Array.isArray(baseline)) {
    return (
      value.length === baseline.length &&
      value.every((model, index) => model === baseline[index])
    )
  }

  return value === baseline
}

const buildFormDefaults = (
  defaults: PoeLogSettingsSectionProps['defaultValues']
): PoeLogFormInput => ({
  poe_log_setting: {
    enabled: defaults['poe_log_setting.enabled'],
    sync_interval: defaults['poe_log_setting.sync_interval'],
    key_deduplicate: defaults['poe_log_setting.key_deduplicate'],
    free_models: Array.isArray(defaults['poe_log_setting.free_models'])
      ? (defaults['poe_log_setting.free_models'] as string[]).join(', ')
      : (defaults['poe_log_setting.free_models'] as string) || '',
    sync_to_consume_log: defaults['poe_log_setting.sync_to_consume_log'],
  },
})

const normalizeDefaults = (
  defaults: PoeLogSettingsSectionProps['defaultValues']
): NormalizedPoeLogValues => ({
  'poe_log_setting.enabled': defaults['poe_log_setting.enabled'],
  'poe_log_setting.sync_interval': defaults['poe_log_setting.sync_interval'],
  'poe_log_setting.key_deduplicate': defaults['poe_log_setting.key_deduplicate'],
  'poe_log_setting.free_models': Array.isArray(
    defaults['poe_log_setting.free_models']
  )
    ? (defaults['poe_log_setting.free_models'] as string[])
    : [],
  'poe_log_setting.sync_to_consume_log':
    defaults['poe_log_setting.sync_to_consume_log'],
})

const normalizeFormValues = (
  values: PoeLogFormValues
): NormalizedPoeLogValues => ({
  'poe_log_setting.enabled': values.poe_log_setting.enabled,
  'poe_log_setting.sync_interval': values.poe_log_setting.sync_interval,
  'poe_log_setting.key_deduplicate': values.poe_log_setting.key_deduplicate,
  'poe_log_setting.sync_to_consume_log':
    values.poe_log_setting.sync_to_consume_log,
  'poe_log_setting.free_models': values.poe_log_setting.free_models
    .split(',')
    .map((model) => model.trim())
    .filter((model) => model !== ''),
})

export function PoeLogSettingsSection(props: PoeLogSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<PoeLogFormInput, unknown, PoeLogFormValues>({
    resolver: zodResolver(poeLogSchema),
    defaultValues: buildFormDefaults(props.defaultValues),
  })

  useResetForm(form, buildFormDefaults(props.defaultValues))

  const onSubmit = async (values: PoeLogFormValues) => {
    const normalized = normalizeFormValues(values)
    const baseline = normalizeDefaults(props.defaultValues)

    const updates = (
      Object.keys(normalized) as Array<keyof NormalizedPoeLogValues>
    ).filter((key) => !areNormalizedValuesEqual(normalized[key], baseline[key]))

    if (updates.length === 0) return

    for (const key of updates) {
      const value = normalized[key]
      await updateOption.mutateAsync({
        key,
        value: Array.isArray(value) ? JSON.stringify(value) : value,
      })
    }
  }

  return (
    <SettingsSection title={t('Poe Log Sync')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save Poe log settings'
          />
          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='poe_log_setting.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable Poe Log Sync')}</FormLabel>
                    <FormDescription>
                      {t('Periodically fetch Poe usage history and store as Poe logs. When disabled, the sync task will not run.')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='poe_log_setting.sync_interval'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Sync interval (seconds)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('How frequently the system fetches Poe usage history, in seconds')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='poe_log_setting.key_deduplicate'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Key deduplication')}</FormLabel>
                    <FormDescription>
                      {t('When enabled, channels sharing the same API key only fetch history once, avoiding duplicate API calls. The results are assigned to all channels sharing that key.')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='poe_log_setting.sync_to_consume_log'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Sync to consume log')}</FormLabel>
                    <FormDescription>
                      {t('When enabled, each synced Poe log entry also creates a consume log record for billing. Disable to only store Poe logs without generating consume log entries.')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='poe_log_setting.free_models'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Free Models')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('e.g. GPT-3.5-Turbo, Claude-Instant')}
                      {...field}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Comma-separated list of model names treated as free (no points charged). These are merged with models auto-detected from the Poe API.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
