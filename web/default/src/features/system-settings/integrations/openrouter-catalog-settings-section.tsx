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

const openRouterCatalogSchema = z.object({
  openrouter_catalog_setting: z.object({
    enabled: z.boolean(),
    refresh_interval: z.coerce
      .number()
      .int()
      .min(30, 'Interval must be at least 30 minutes'),
    models_url: z.string(),
    proxy: z.string(),
  }),
})

type OpenRouterCatalogFormValues = z.output<typeof openRouterCatalogSchema>
type OpenRouterCatalogFormInput = z.input<typeof openRouterCatalogSchema>

type OpenRouterCatalogSettingsSectionProps = {
  defaultValues: {
    'openrouter_catalog_setting.enabled': boolean
    'openrouter_catalog_setting.refresh_interval': number
    'openrouter_catalog_setting.models_url': string
    'openrouter_catalog_setting.proxy': string
  }
}

type NormalizedOpenRouterCatalogValues = {
  'openrouter_catalog_setting.enabled': boolean
  'openrouter_catalog_setting.refresh_interval': number
  'openrouter_catalog_setting.models_url': string
  'openrouter_catalog_setting.proxy': string
}

const buildFormDefaults = (
  defaults: OpenRouterCatalogSettingsSectionProps['defaultValues']
): OpenRouterCatalogFormInput => ({
  openrouter_catalog_setting: {
    enabled: defaults['openrouter_catalog_setting.enabled'],
    refresh_interval: defaults['openrouter_catalog_setting.refresh_interval'],
    models_url: defaults['openrouter_catalog_setting.models_url'],
    proxy: defaults['openrouter_catalog_setting.proxy'],
  },
})

const normalizeDefaults = (
  defaults: OpenRouterCatalogSettingsSectionProps['defaultValues']
): NormalizedOpenRouterCatalogValues => ({
  'openrouter_catalog_setting.enabled':
    defaults['openrouter_catalog_setting.enabled'],
  'openrouter_catalog_setting.refresh_interval':
    defaults['openrouter_catalog_setting.refresh_interval'],
  'openrouter_catalog_setting.models_url':
    defaults['openrouter_catalog_setting.models_url'],
  'openrouter_catalog_setting.proxy':
    defaults['openrouter_catalog_setting.proxy'],
})

const normalizeFormValues = (
  values: OpenRouterCatalogFormValues
): NormalizedOpenRouterCatalogValues => ({
  'openrouter_catalog_setting.enabled':
    values.openrouter_catalog_setting.enabled,
  'openrouter_catalog_setting.refresh_interval':
    values.openrouter_catalog_setting.refresh_interval,
  'openrouter_catalog_setting.models_url':
    values.openrouter_catalog_setting.models_url.trim(),
  'openrouter_catalog_setting.proxy':
    values.openrouter_catalog_setting.proxy.trim(),
})

export function OpenRouterCatalogSettingsSection(
  props: OpenRouterCatalogSettingsSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<
    OpenRouterCatalogFormInput,
    unknown,
    OpenRouterCatalogFormValues
  >({
    resolver: zodResolver(openRouterCatalogSchema),
    defaultValues: buildFormDefaults(props.defaultValues),
  })

  useResetForm(form, buildFormDefaults(props.defaultValues))

  const onSubmit = async (values: OpenRouterCatalogFormValues) => {
    const normalized = normalizeFormValues(values)
    const baseline = normalizeDefaults(props.defaultValues)

    const updates = (
      Object.keys(normalized) as Array<keyof NormalizedOpenRouterCatalogValues>
    ).filter((key) => normalized[key] !== baseline[key])

    if (updates.length === 0) return

    for (const key of updates) {
      await updateOption.mutateAsync({ key, value: normalized[key] })
    }
  }

  return (
    <SettingsSection title={t('OpenRouter Model Metadata')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save OpenRouter model metadata settings'
          />
          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='openrouter_catalog_setting.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>
                      {t('Enable OpenRouter Model Metadata')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'Periodically fetch context length, output limit, modalities, capabilities and release dates from OpenRouter for the model pricing pages. When disabled, the fetch task will not run.'
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
              name='openrouter_catalog_setting.refresh_interval'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Refresh interval (minutes)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={30}
                      step={1}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Minimum 30 minutes to avoid excessive requests to OpenRouter'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6'>
            <FormField
              control={form.control}
              name='openrouter_catalog_setting.models_url'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Models URL')}</FormLabel>
                  <FormControl>
                    <Input
                      type='url'
                      inputMode='url'
                      placeholder='https://openrouter.ai/api/v1/models'
                      autoComplete='off'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'OpenRouter-compatible models endpoint used to fetch model metadata.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6'>
            <FormField
              control={form.control}
              name='openrouter_catalog_setting.proxy'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Proxy URL (optional)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='text'
                      inputMode='url'
                      placeholder='http://127.0.0.1:7890'
                      autoComplete='off'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'HTTP proxy used when fetching model metadata. Leave empty to connect directly.'
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
