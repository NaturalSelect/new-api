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
import { useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { getIntelligenceScores } from '@/features/dashboard/api'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Checkbox } from '@/components/ui/checkbox'
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

const intelligenceSchema = z.object({
  intelligence_setting: z.object({
    enabled: z.boolean(),
    refresh_interval: z.coerce
      .number()
      .int()
      .min(10, 'Interval must be at least 10 minutes'),
    auto_effort_enabled: z.boolean(),
    disabled_auto_efforts: z.array(z.string()),
  }),
})

type IntelligenceFormValues = z.output<typeof intelligenceSchema>
type IntelligenceFormInput = z.input<typeof intelligenceSchema>

type IntelligenceSettingsSectionProps = {
  defaultValues: {
    'intelligence_setting.enabled': boolean
    'intelligence_setting.refresh_interval': number
    'intelligence_setting.auto_effort_enabled': boolean
    'intelligence_setting.disabled_auto_efforts': string[]
  }
}

type NormalizedIntelligenceValues = {
  'intelligence_setting.enabled': boolean
  'intelligence_setting.refresh_interval': number
  'intelligence_setting.auto_effort_enabled': boolean
  'intelligence_setting.disabled_auto_efforts': string[]
}

const buildFormDefaults = (
  defaults: IntelligenceSettingsSectionProps['defaultValues']
): IntelligenceFormInput => ({
  intelligence_setting: {
    enabled: defaults['intelligence_setting.enabled'],
    refresh_interval: defaults['intelligence_setting.refresh_interval'],
    auto_effort_enabled: defaults['intelligence_setting.auto_effort_enabled'],
    disabled_auto_efforts:
      defaults['intelligence_setting.disabled_auto_efforts'],
  },
})

const normalizeDefaults = (
  defaults: IntelligenceSettingsSectionProps['defaultValues']
): NormalizedIntelligenceValues => ({
  'intelligence_setting.enabled': defaults['intelligence_setting.enabled'],
  'intelligence_setting.refresh_interval':
    defaults['intelligence_setting.refresh_interval'],
  'intelligence_setting.auto_effort_enabled':
    defaults['intelligence_setting.auto_effort_enabled'],
  'intelligence_setting.disabled_auto_efforts':
    defaults['intelligence_setting.disabled_auto_efforts'],
})

const normalizeFormValues = (
  values: IntelligenceFormValues
): NormalizedIntelligenceValues => ({
  'intelligence_setting.enabled': values.intelligence_setting.enabled,
  'intelligence_setting.refresh_interval':
    values.intelligence_setting.refresh_interval,
  'intelligence_setting.auto_effort_enabled':
    values.intelligence_setting.auto_effort_enabled,
  'intelligence_setting.disabled_auto_efforts':
    values.intelligence_setting.disabled_auto_efforts,
})

export function IntelligenceSettingsSection(
  props: IntelligenceSettingsSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const scoresQuery = useQuery({
    queryKey: ['dashboard', 'intelligence-scores'],
    queryFn: getIntelligenceScores,
    staleTime: 60_000,
  })

  const knownEfforts = useMemo(() => {
    const scores = scoresQuery.data?.data.scores ?? []
    return Array.from(new Set(scores.map((score) => score.effort))).sort()
  }, [scoresQuery.data])

  const form = useForm<
    IntelligenceFormInput,
    unknown,
    IntelligenceFormValues
  >({
    resolver: zodResolver(intelligenceSchema),
    defaultValues: buildFormDefaults(props.defaultValues),
  })

  useResetForm(form, buildFormDefaults(props.defaultValues))

  const onSubmit = async (values: IntelligenceFormValues) => {
    const normalized = normalizeFormValues(values)
    const baseline = normalizeDefaults(props.defaultValues)

    const updates = (
      Object.keys(normalized) as Array<keyof NormalizedIntelligenceValues>
    ).filter((key) => normalized[key] !== baseline[key])

    if (updates.length === 0) return

    for (const key of updates) {
      await updateOption.mutateAsync({ key, value: normalized[key] })
    }
  }

  return (
    <SettingsSection title={t('Intelligence Sync')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save intelligence sync settings'
          />
          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='intelligence_setting.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable Intelligence Sync')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Periodically fetch model intelligence scores from an external benchmark API for the dashboard chart. When disabled, the sync task will not run.'
                      )}
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
              name='intelligence_setting.refresh_interval'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Refresh interval (minutes)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={10}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Minimum 10 minutes to avoid excessive requests to the external API'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='intelligence_setting.auto_effort_enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable Auto Effort Resolution')}</FormLabel>
                    <FormDescription>
                      {t(
                        'When enabled and a request specifies reasoning effort as "auto", the system will automatically select the highest-IQ effort level from the intelligence scores for that model. Requires Intelligence Sync to be active.'
                      )}
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

          <div className='grid gap-6'>
            <FormField
              control={form.control}
              name='intelligence_setting.disabled_auto_efforts'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Disabled Effort Levels for Auto Selection')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Effort levels checked here will be excluded from automatic selection. Useful for disabling expensive tiers like "ultra".'
                    )}
                  </FormDescription>
                  <FormControl>
                    {knownEfforts.length === 0 ? (
                      <p className='text-muted-foreground text-sm'>
                        {t(
                          'No effort levels available. Enable Intelligence Sync and wait for data to load.'
                        )}
                      </p>
                    ) : (
                      <div className='flex flex-wrap gap-4'>
                        {knownEfforts.map((effort) => {
                          const checked = field.value.includes(effort)
                          return (
                            <label
                              key={effort}
                              className='flex items-center gap-2 text-sm'
                            >
                              <Checkbox
                                checked={checked}
                                onCheckedChange={(next) => {
                                  if (next === true) {
                                    field.onChange([...field.value, effort])
                                  } else {
                                    field.onChange(
                                      field.value.filter((e) => e !== effort)
                                    )
                                  }
                                }}
                              />
                              {effort}
                            </label>
                          )
                        })}
                      </div>
                    )}
                  </FormControl>
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
