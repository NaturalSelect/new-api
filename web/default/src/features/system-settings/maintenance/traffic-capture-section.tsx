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
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { RefreshCw, ShieldAlert, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatBytes, formatNumber, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
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
import { Progress } from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { clearTrafficCapture, getTrafficCaptureStats } from '../api'
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

const MINUTES_PER_HOUR = 60
const MINUTES_PER_DAY = 24 * MINUTES_PER_HOUR

// NOTE: an empty allowlist means "capture every user", so a malformed entry
// must be rejected instead of silently dropped, or the scope would widen.
const parseUserIds = (value: string): number[] | null => {
  const tokens = value.split(/[\s,，]+/).filter(Boolean)
  if (!tokens.every((token) => /^[1-9]\d{0,9}$/.test(token))) return null
  return Array.from(new Set(tokens.map(Number)))
}

/**
 * react-hook-form 7 interprets dotted `name` strings as nested paths, so the
 * form is modelled with nested objects and flattened back to the server-side
 * option keys right before persisting (same approach as PerformanceSection).
 */
const trafficCaptureSchema = z.object({
  traffic_capture_setting: z.object({
    enabled: z.boolean(),
    capture_upstream: z.boolean(),
    record_mode: z.enum(['all', 'failed']),
    user_ids: z
      .string()
      .refine(
        (value) => parseUserIds(value) !== null,
        'User IDs must be positive integers separated by commas'
      ),
    ttl_minutes: z.coerce
      .number()
      .int('Must be a whole number')
      .min(1, 'Must be at least 1'),
    retention_size_mb: z.coerce
      .number()
      .int('Must be a whole number')
      .min(1, 'Must be at least 1'),
    max_body_mb: z.coerce
      .number()
      .int('Must be a whole number')
      .min(1, 'Must be at least 1'),
  }),
})

type TrafficCaptureFormInput = z.input<typeof trafficCaptureSchema>
type TrafficCaptureFormValues = z.output<typeof trafficCaptureSchema>

type TrafficCaptureValues = {
  'traffic_capture_setting.enabled': boolean
  'traffic_capture_setting.capture_upstream': boolean
  'traffic_capture_setting.record_mode': 'all' | 'failed'
  'traffic_capture_setting.user_ids': number[]
  'traffic_capture_setting.ttl_minutes': number
  'traffic_capture_setting.retention_size_mb': number
  'traffic_capture_setting.max_body_mb': number
}

type TrafficCaptureSectionProps = {
  defaultValues: TrafficCaptureValues
}

const buildFormDefaults = (
  defaults: TrafficCaptureValues
): TrafficCaptureFormInput => ({
  traffic_capture_setting: {
    enabled: defaults['traffic_capture_setting.enabled'],
    capture_upstream: defaults['traffic_capture_setting.capture_upstream'],
    record_mode: defaults['traffic_capture_setting.record_mode'],
    user_ids: defaults['traffic_capture_setting.user_ids'].join(', '),
    ttl_minutes: defaults['traffic_capture_setting.ttl_minutes'],
    retention_size_mb: defaults['traffic_capture_setting.retention_size_mb'],
    max_body_mb: defaults['traffic_capture_setting.max_body_mb'],
  },
})

const normalizeFormValues = (
  values: TrafficCaptureFormValues
): TrafficCaptureValues => ({
  'traffic_capture_setting.enabled': values.traffic_capture_setting.enabled,
  'traffic_capture_setting.capture_upstream':
    values.traffic_capture_setting.capture_upstream,
  'traffic_capture_setting.record_mode':
    values.traffic_capture_setting.record_mode,
  'traffic_capture_setting.user_ids':
    parseUserIds(values.traffic_capture_setting.user_ids) ?? [],
  'traffic_capture_setting.ttl_minutes':
    values.traffic_capture_setting.ttl_minutes,
  'traffic_capture_setting.retention_size_mb':
    values.traffic_capture_setting.retention_size_mb,
  'traffic_capture_setting.max_body_mb':
    values.traffic_capture_setting.max_body_mb,
})

function formatTtlSeconds(seconds: number, t: TFunction): string {
  if (seconds <= 0) return '-'
  const totalMinutes = Math.max(1, Math.round(seconds / 60))
  const days = Math.floor(totalMinutes / MINUTES_PER_DAY)
  const hours = Math.floor((totalMinutes % MINUTES_PER_DAY) / MINUTES_PER_HOUR)
  const minutes = totalMinutes % MINUTES_PER_HOUR
  return [
    days > 0 && t('{{value}}d', { value: days }),
    hours > 0 && t('{{value}}h', { value: hours }),
    minutes > 0 && t('{{value}}min', { value: minutes }),
  ]
    .filter(Boolean)
    .join(' ')
}

function StatRow(props: {
  label: string
  value: ReactNode
  hint?: string
  danger?: boolean
}) {
  return (
    <div className='flex items-start justify-between gap-3'>
      <dt className='min-w-0 space-y-0.5'>
        <span className='text-muted-foreground text-xs'>{props.label}</span>
        {props.hint && (
          <span className='text-muted-foreground/70 block text-[11px] leading-snug'>
            {props.hint}
          </span>
        )}
      </dt>
      <dd
        className={cn(
          'shrink-0 text-right text-xs font-medium',
          props.danger && 'text-destructive'
        )}
      >
        {props.value}
      </dd>
    </div>
  )
}

function TrafficCaptureStatsPanel() {
  const { t } = useTranslation()
  const [clearDialogOpen, setClearDialogOpen] = useState(false)

  const statsQuery = useQuery({
    queryKey: ['traffic-capture-stats'],
    queryFn: getTrafficCaptureStats,
  })
  const stats = statsQuery.data?.data

  const clearMutation = useMutation({
    mutationFn: clearTrafficCapture,
    onSuccess: (res) => {
      if (!res.success) return
      toast.success(
        t('{{count}} captured records removed.', { count: res.data ?? 0 })
      )
      setClearDialogOpen(false)
      statsQuery.refetch()
    },
  })

  const usagePercent =
    stats && stats.retention_size_bytes > 0
      ? Math.min(
          100,
          Math.round((stats.stored_bytes / stats.retention_size_bytes) * 100)
        )
      : 0

  return (
    <div className='space-y-4 rounded-lg border p-4'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h4 className='font-medium'>{t('Capture statistics')}</h4>
        <div className='flex items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => statsQuery.refetch()}
            disabled={statsQuery.isFetching}
          >
            <RefreshCw
              data-icon='inline-start'
              className={cn(statsQuery.isFetching && 'animate-spin')}
            />
            {t('Refresh')}
          </Button>
          <Button
            type='button'
            variant='destructive'
            size='sm'
            onClick={() => setClearDialogOpen(true)}
          >
            <Trash2 data-icon='inline-start' />
            {t('Clear all records')}
          </Button>
        </div>
      </div>

      {stats ? (
        <div className='space-y-4'>
          <div className='space-y-1.5'>
            <Progress value={usagePercent} />
            <div className='text-muted-foreground flex justify-between text-xs'>
              <span>
                {formatBytes(stats.stored_bytes)} /{' '}
                {formatBytes(stats.retention_size_bytes)}
              </span>
              <span>{usagePercent}%</span>
            </div>
          </div>
          <Separator />
          <dl className='space-y-2.5'>
            <StatRow label={t('Records')} value={formatNumber(stats.count)} />
            <StatRow
              label={t('Oldest record')}
              value={formatTimestampToDate(stats.oldest_created_at)}
            />
            <StatRow
              label={t('Retention time')}
              value={formatTtlSeconds(stats.ttl_seconds, t)}
            />
            <StatRow
              label={t('Dropped records')}
              hint={t(
                'Dropped because the write queue was full, since the server started'
              )}
              value={formatNumber(stats.dropped)}
              danger={stats.dropped > 0}
            />
            <StatRow
              label={t('Write failures')}
              value={formatNumber(stats.write_failures)}
              danger={stats.write_failures > 0}
            />
            <StatRow
              label={t('Last cleanup')}
              hint={
                stats.last_cleanup_at > 0
                  ? t('{{count}} records removed', {
                      count: stats.last_cleanup_deleted,
                    })
                  : undefined
              }
              value={
                stats.last_cleanup_at > 0
                  ? formatTimestampToDate(stats.last_cleanup_at)
                  : t('Not yet')
              }
            />
          </dl>
        </div>
      ) : (
        <p className='text-muted-foreground text-sm'>
          {statsQuery.isLoading ? t('Loading...') : t('Failed to load')}
        </p>
      )}

      <ConfirmDialog
        open={clearDialogOpen}
        onOpenChange={setClearDialogOpen}
        title={t('Clear all captured records?')}
        desc={t(
          'This will permanently delete all captured traffic records. This action cannot be undone.'
        )}
        confirmText={t('Clear all records')}
        destructive
        isLoading={clearMutation.isPending}
        handleConfirm={() => clearMutation.mutate()}
      />
    </div>
  )
}

export function TrafficCaptureSection(props: TrafficCaptureSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo(
    () => buildFormDefaults(props.defaultValues),
    [props.defaultValues]
  )

  const form = useForm<
    TrafficCaptureFormInput,
    unknown,
    TrafficCaptureFormValues
  >({
    resolver: zodResolver(trafficCaptureSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const enabled = form.watch('traffic_capture_setting.enabled')
  const currentTtlMinutes = Number(
    form.watch('traffic_capture_setting.ttl_minutes')
  )

  const ttlPresets = [
    { label: t('1h'), minutes: 60 },
    { label: t('6h'), minutes: 6 * 60 },
    { label: t('24h'), minutes: 24 * 60 },
    { label: t('72h'), minutes: 72 * 60 },
    { label: t('7d'), minutes: 7 * 24 * 60 },
  ]

  const onSubmit = async (values: TrafficCaptureFormValues) => {
    const normalized = normalizeFormValues(values)
    const changedKeys = (
      Object.keys(normalized) as Array<keyof TrafficCaptureValues>
    ).filter(
      (key) =>
        JSON.stringify(normalized[key]) !==
        JSON.stringify(props.defaultValues[key])
    )

    if (changedKeys.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of changedKeys) {
      const value = normalized[key]
      await updateOption.mutateAsync({
        key,
        value: Array.isArray(value) ? JSON.stringify(value) : value,
      })
    }
  }

  return (
    <SettingsSection title={t('Traffic Capture')}>
      <div className='grid items-start gap-6 xl:grid-cols-[minmax(0,1fr)_22rem]'>
        <Form {...form}>
          <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
            <SettingsPageFormActions
              onSave={form.handleSubmit(onSubmit)}
              isSaving={updateOption.isPending}
              saveLabel='Save traffic capture settings'
            />

            <div>
              <h4 className='font-medium'>{t('Capture')}</h4>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t(
                  'Record the full client request and response, plus every attempt sent to upstream channels. Only Root users can configure or view captured traffic.'
                )}
              </p>
            </div>

            <Alert variant='destructive'>
              <ShieldAlert />
              <AlertDescription>
                {t(
                  'Request and response bodies are stored as-is and are not redacted. Only secrets in request headers and URLs are masked. Captured data is written to the log database.'
                )}
              </AlertDescription>
            </Alert>

            <FormField
              control={form.control}
              name='traffic_capture_setting.enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable traffic capture')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Capture relay requests and responses that match the rules below.'
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
              name='traffic_capture_setting.capture_upstream'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Capture upstream attempts')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Also record every request sent to upstream channels, including retries.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                      disabled={!enabled}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />

            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='traffic_capture_setting.record_mode'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Record mode')}</FormLabel>
                    <Select
                      items={[
                        { value: 'all', label: t('Record all requests') },
                        {
                          value: 'failed',
                          label: t('Record failed requests only'),
                        },
                      ]}
                      value={field.value}
                      onValueChange={field.onChange}
                      disabled={!enabled}
                    >
                      <FormControl>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='all'>
                            {t('Record all requests')}
                          </SelectItem>
                          <SelectItem value='failed'>
                            {t('Record failed requests only')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='traffic_capture_setting.max_body_mb'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Max body size (MB)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        {...safeNumberFieldProps(field)}
                        disabled={!enabled}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Bodies larger than this are truncated.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='traffic_capture_setting.user_ids'
                render={({ field }) => (
                  <FormItem className='md:col-span-2'>
                    <FormLabel>{t('User ID allowlist')}</FormLabel>
                    <FormControl>
                      <Input
                        placeholder={t('e.g. 1, 2, 3')}
                        value={field.value ?? ''}
                        onChange={(event) => field.onChange(event.target.value)}
                        name={field.name}
                        onBlur={field.onBlur}
                        ref={field.ref}
                        disabled={!enabled}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Comma-separated user IDs. Leave empty to capture all users.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>

            <Separator />

            <div>
              <h4 className='font-medium'>{t('Retention')}</h4>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t(
                  'Records are cleaned up automatically once they exceed the retention time or the retention size. The size limit is measured on the compressed size. Changes take effect within 1 minute, no restart required.'
                )}
              </p>
            </div>

            <div className='grid grid-cols-1 gap-4 md:grid-cols-2'>
              <FormField
                control={form.control}
                name='traffic_capture_setting.ttl_minutes'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Retention time (minutes)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <div className='flex flex-wrap gap-1.5'>
                      {ttlPresets.map((preset) => (
                        <Button
                          key={preset.minutes}
                          type='button'
                          size='xs'
                          variant={
                            currentTtlMinutes === preset.minutes
                              ? 'secondary'
                              : 'outline'
                          }
                          onClick={() =>
                            form.setValue(
                              'traffic_capture_setting.ttl_minutes',
                              preset.minutes,
                              { shouldDirty: true, shouldValidate: true }
                            )
                          }
                        >
                          {preset.label}
                        </Button>
                      ))}
                    </div>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='traffic_capture_setting.retention_size_mb'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Retention size (MB)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Maximum total compressed size of captured records.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </SettingsForm>
        </Form>

        <TrafficCaptureStatsPanel />
      </div>
    </SettingsSection>
  )
}
