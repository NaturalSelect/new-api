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
import { useMemo } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormLabel,
} from '@/components/ui/form'
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

/**
 * Use a nested object so the dotted FormField `name` prop lines up with
 * react-hook-form's path semantics (see passkey-section.tsx for the same
 * pattern applied to a multi-field settings group).
 */
const secureVerificationSchema = z.object({
  secure_verification: z.object({
    require_for_channel_key: z.boolean(),
    require_password_for_channel_key: z.boolean(),
  }),
})

type SecureVerificationFormValues = z.infer<typeof secureVerificationSchema>

type FlatSecureVerificationDefaults = {
  'secure_verification.require_for_channel_key': boolean
  'secure_verification.require_password_for_channel_key': boolean
}

const buildFormDefaults = (
  defaults: FlatSecureVerificationDefaults
): SecureVerificationFormValues => ({
  secure_verification: {
    require_for_channel_key:
      defaults['secure_verification.require_for_channel_key'],
    require_password_for_channel_key:
      defaults['secure_verification.require_password_for_channel_key'],
  },
})

interface SecureVerificationSectionProps {
  defaultValues: FlatSecureVerificationDefaults
}

export function SecureVerificationSection(
  props: SecureVerificationSectionProps
) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<SecureVerificationFormValues>({
    resolver: zodResolver(secureVerificationSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const onSubmit = async (values: SecureVerificationFormValues) => {
    const updates: {
      key: keyof FlatSecureVerificationDefaults
      value: boolean
    }[] = [
      {
        key: 'secure_verification.require_for_channel_key',
        value: values.secure_verification.require_for_channel_key,
      },
      {
        key: 'secure_verification.require_password_for_channel_key',
        value: values.secure_verification.require_password_for_channel_key,
      },
    ]

    for (const update of updates) {
      if (update.value === props.defaultValues[update.key]) continue
      await updateOption.mutateAsync({ key: update.key, value: update.value })
    }
  }

  return (
    <SettingsSection title={t('Secure Verification')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />
          <FormField
            control={form.control}
            name='secure_verification.require_for_channel_key'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Require Verification for Channel Key')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      "Require 2FA or Passkey verification before revealing a channel's API key. When disabled, admins can view channel keys without additional verification."
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
            name='secure_verification.require_password_for_channel_key'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Require Password for Channel Key')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      "Require admins to re-enter their login password before revealing a channel's API key. Can be enabled together with verification, in which case both checks must pass."
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
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
