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
import { useEffect, useMemo, useRef } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { useUpdateOption } from '../hooks/use-update-option'

/**
 * The schema uses a nested object so the dotted FormField `name` props line
 * up with react-hook-form's path semantics. Using flat keys like
 * `'easter_egg_setting.enabled'` causes RHF to silently maintain two
 * parallel value trees and saves never see the user input.
 */
const easterEggSchema = z.object({
  easter_egg_setting: z.object({
    enabled: z.boolean(),
    model_name: z.string().trim(),
  }),
})

type EasterEggFormInput = z.input<typeof easterEggSchema>
type EasterEggFormValues = z.output<typeof easterEggSchema>

type FlatEasterEggDefaults = {
  'easter_egg_setting.enabled': boolean
  'easter_egg_setting.model_name': string
}

const buildFormDefaults = (
  defaults: FlatEasterEggDefaults
): EasterEggFormInput => ({
  easter_egg_setting: {
    enabled: defaults['easter_egg_setting.enabled'],
    model_name: defaults['easter_egg_setting.model_name'],
  },
})

const normalizeFormValues = (
  values: EasterEggFormValues
): FlatEasterEggDefaults => ({
  'easter_egg_setting.enabled': values.easter_egg_setting.enabled,
  'easter_egg_setting.model_name': values.easter_egg_setting.model_name,
})

interface Props {
  defaultValues: FlatEasterEggDefaults
}

export function EasterEggSettingsCard(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<EasterEggFormInput, unknown, EasterEggFormValues>({
    resolver: zodResolver(easterEggSchema),
    defaultValues: formDefaults,
  })

  const baselineRef = useRef<FlatEasterEggDefaults>(props.defaultValues)
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(props.defaultValues)
  )

  useEffect(() => {
    const serialized = JSON.stringify(props.defaultValues)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = props.defaultValues
    baselineSerializedRef.current = serialized
    form.reset(buildFormDefaults(props.defaultValues))
  }, [props.defaultValues, form])

  const onSubmit = async (values: EasterEggFormValues) => {
    const normalized = normalizeFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof FlatEasterEggDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
    form.reset(buildFormDefaults(normalized))
  }

  const enabled = form.watch('easter_egg_setting.enabled')

  return (
    <SettingsSection title={t('Easter Egg Model')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='easter_egg_setting.enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable easter egg model')}</FormLabel>
                  <FormDescription>
                    {t(
                      'When enabled, requests using the configured model name are answered locally with a fixed ASCII art reply, without calling any upstream channel and without consuming quota.'
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

          <FormField
            control={form.control}
            name='easter_egg_setting.model_name'
            render={({ field }) => (
              <FormItem className='max-w-xs'>
                <FormLabel>{t('Easter egg model name')}</FormLabel>
                <FormControl>
                  <Input placeholder='nailong' {...field} disabled={!enabled} />
                </FormControl>
                <FormDescription>
                  {t(
                    'Matched case-insensitively. Use a name that does not belong to any real model, otherwise all requests to that model will be hijacked by the easter egg.'
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
