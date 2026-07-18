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
import { zodResolver } from '@hookform/resolvers/zod'
import { useForm } from 'react-hook-form'
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

const createNodeStudioSchema = (t: (key: string) => string) =>
  z.object({
    enabled: z.boolean(),
    url: z.string().refine((value) => {
      try {
        const parsed = new URL(value.trim())
        return (
          (parsed.protocol === 'http:' || parsed.protocol === 'https:') &&
          parsed.host !== ''
        )
      } catch {
        return false
      }
    }, t('Provide a valid URL starting with http:// or https://')),
    secret: z.string(),
  })

type NodeStudioFormValues = z.infer<
  ReturnType<typeof createNodeStudioSchema>
>

type NodeStudioSettingsSectionProps = {
  defaultValues: NodeStudioFormValues
}

export function NodeStudioSettingsSection({
  defaultValues,
}: NodeStudioSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const nodeStudioSchema = createNodeStudioSchema(t)
  const form = useForm<NodeStudioFormValues>({
    resolver: zodResolver(nodeStudioSchema),
    defaultValues,
  })

  useResetForm(form, defaultValues)

  const onSubmit = async (values: NodeStudioFormValues) => {
    const enabled = values.enabled
    const url = values.url.trim()
    const secret = values.secret.trim()
    const wasEnabled = defaultValues.enabled
    const initialURL = defaultValues.url.trim()

    if (!enabled && wasEnabled) {
      await updateOption.mutateAsync({
        key: 'node_studio.enabled',
        value: false,
      })
    }
    if (url !== initialURL) {
      await updateOption.mutateAsync({ key: 'node_studio.url', value: url })
    }
    if (secret !== '') {
      await updateOption.mutateAsync({
        key: 'node_studio.secret',
        value: secret,
      })
    }
    if (enabled && !wasEnabled) {
      await updateOption.mutateAsync({
        key: 'node_studio.enabled',
        value: true,
      })
    }
  }

  return (
    <SettingsSection title={t('Node Studio')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
            saveLabel='Save Node Studio settings'
          />

          <FormField
            control={form.control}
            name='enabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable Node Studio')}</FormLabel>
                  <FormDescription>
                    {t(
                      'Show Node Studio in the header after the receiving URL and shared secret are configured.'
                    )}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
                <FormMessage />
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='url'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Node Studio receiving URL')}</FormLabel>
                <FormControl>
                  <Input
                    type='url'
                    inputMode='url'
                    autoComplete='off'
                    placeholder='https://node.dstopology.com/auth/import-keys'
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'The Node backend endpoint that accepts the encrypted payload by form POST.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='secret'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Shared secret')}</FormLabel>
                <FormControl>
                  <Input
                    type='password'
                    autoComplete='new-password'
                    placeholder={t('Enter new key to update')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Enter the same secret in new-api and Node Studio. Leave blank to keep the existing secret.'
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
