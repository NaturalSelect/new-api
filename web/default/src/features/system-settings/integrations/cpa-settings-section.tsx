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
import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'
import { removeTrailingSlash } from './utils'

const CPA_MIN_SYNC_INTERVAL_SECONDS = 30

const createCPASchema = (t: (key: string) => string) =>
  z.object({
    CPAUrl: z.string().refine((value) => {
      const trimmed = value.trim()
      if (!trimmed) return true
      return /^https?:\/\//.test(trimmed)
    }, t('Provide a valid URL starting with http:// or https://')),
    CPAManagementKey: z.string(),
    CPASyncInterval: z
      .number()
      .int()
      .min(
        CPA_MIN_SYNC_INTERVAL_SECONDS,
        t('Interval must be at least 30 seconds')
      ),
  })

type CPAFormValues = z.infer<ReturnType<typeof createCPASchema>>

type CPASettingsSectionProps = {
  defaultValues: CPAFormValues
}

export function CPASettingsSection({
  defaultValues,
}: CPASettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const cpaSchema = createCPASchema(t)

  const form = useForm<CPAFormValues>({
    resolver: zodResolver(cpaSchema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (values: CPAFormValues) => {
    const sanitizedUrl = removeTrailingSlash(values.CPAUrl)
    const sanitizedKey = values.CPAManagementKey.trim()
    const initialUrl = removeTrailingSlash(defaultValues.CPAUrl)
    const initialKey = defaultValues.CPAManagementKey.trim()

    const updates: Array<{ key: string; value: string | number }> = []

    if (sanitizedUrl !== initialUrl) {
      updates.push({ key: 'CPAUrl', value: sanitizedUrl })
    }

    if (sanitizedKey !== initialKey || sanitizedUrl === '') {
      updates.push({ key: 'CPAManagementKey', value: sanitizedKey })
    }

    if (values.CPASyncInterval !== defaultValues.CPASyncInterval) {
      updates.push({ key: 'CPASyncInterval', value: values.CPASyncInterval })
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }
  }

  return (
    <SettingsSection title={t('CPA Service')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save CPA settings'
          />
          <FormField
            control={form.control}
            name='CPAUrl'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('CPA URL')}</FormLabel>
                <FormControl>
                  <Input
                    type='url'
                    inputMode='url'
                    placeholder={t('http://127.0.0.1:8317')}
                    autoComplete='off'
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Base URL of the CPA service used to fetch credential usage snapshots. Trailing slashes are removed automatically.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='CPAManagementKey'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('CPA Management Key')}</FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    placeholder={t('Enter new key to update')}
                    autoComplete='new-password'
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    "The CPA service's management key (remote-management.secret-key). Leave blank to keep the existing secret."
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='CPASyncInterval'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Sync Interval (seconds)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={CPA_MIN_SYNC_INTERVAL_SECONDS}
                    step={1}
                    {...safeNumberFieldProps(field)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'How often to fetch usage snapshots from the CPA service. Minimum 30 seconds.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
