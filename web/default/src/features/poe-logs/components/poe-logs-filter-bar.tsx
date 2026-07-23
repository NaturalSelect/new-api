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
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useIsFetching, useMutation, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { type Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useIsAdmin } from '@/hooks/use-admin'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'
import { clearPoeLogs } from '../api'
import type { PoeLogsFilters } from '../types'
import { PoeLogsStats } from './poe-logs-stats'

const route = getRouteApi('/_authenticated/poe-logs/')
const USAGE_TYPE_ALL_VALUE = '__all__'
const usageTypeValues = [
  USAGE_TYPE_ALL_VALUE,
  'Chat',
  'API',
  'Canvas App',
] as const

type UsageTypeValue = (typeof usageTypeValues)[number]

interface PoeLogsFilterBarProps<TData> {
  table: Table<TData>
}

function isUsageTypeValue(value: string): value is UsageTypeValue {
  return (usageTypeValues as readonly string[]).includes(value)
}

function getDefaultTimeRange(): { start: Date; end: Date } {
  const now = new Date()
  const start = new Date(now)
  start.setHours(0, 0, 0, 0)
  const end = new Date(now)
  end.setHours(23, 59, 59, 999)
  return { start, end }
}

function getDate(value?: number, fallback?: Date): Date | undefined {
  if (value) return new Date(value)
  return fallback
}

export function PoeLogsFilterBar<TData>(props: PoeLogsFilterBarProps<TData>) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const searchParams = route.useSearch()
  const isAdmin = useIsAdmin()
  const fetchingLogs = useIsFetching({ queryKey: ['poe-logs'] })
  const [clearDialogOpen, setClearDialogOpen] = useState(false)
  const [filters, setFilters] = useState<PoeLogsFilters>(() => {
    const { start, end } = getDefaultTimeRange()
    return {
      paidOnly: true,
      startTime: start,
      endTime: end,
    }
  })
  const [usageType, setUsageType] = useState<UsageTypeValue>(
    USAGE_TYPE_ALL_VALUE
  )

  useEffect(() => {
    const { start: defaultStart, end: defaultEnd } = getDefaultTimeRange()
    setFilters({
      startTime: getDate(searchParams.startTime, defaultStart),
      endTime: getDate(searchParams.endTime, defaultEnd),
      channelId: searchParams.channel_id || undefined,
      botName: searchParams.bot_name || undefined,
      usageType: searchParams.usage_type || undefined,
      paidOnly: searchParams.paid_only ?? true,
    })
    setUsageType(
      searchParams.usage_type && isUsageTypeValue(searchParams.usage_type)
        ? searchParams.usage_type
        : USAGE_TYPE_ALL_VALUE
    )
  }, [
    searchParams.bot_name,
    searchParams.channel_id,
    searchParams.endTime,
    searchParams.paid_only,
    searchParams.startTime,
    searchParams.usage_type,
  ])

  const handleChange = useCallback(
    (field: keyof PoeLogsFilters, value: Date | string | boolean | undefined) => {
      setFilters((prev) => ({ ...prev, [field]: value }))
    },
    []
  )

  const handleApply = useCallback(() => {
    void navigate({
      to: '/poe-logs',
      search: {
        p: 1,
        page_size: searchParams.page_size,
        channel_id: filters.channelId || undefined,
        bot_name: filters.botName || undefined,
        usage_type:
          usageType === USAGE_TYPE_ALL_VALUE ? undefined : usageType,
        paid_only: filters.paidOnly ?? true,
        startTime: filters.startTime?.getTime(),
        endTime: filters.endTime?.getTime(),
      },
    })
    queryClient.invalidateQueries({ queryKey: ['poe-logs'] })
    queryClient.invalidateQueries({ queryKey: ['poe-logs-stats'] })
  }, [filters, navigate, queryClient, searchParams.page_size, usageType])

  const handleReset = useCallback(() => {
    const { start, end } = getDefaultTimeRange()
    setFilters({
      paidOnly: true,
      startTime: start,
      endTime: end,
    })
    setUsageType(USAGE_TYPE_ALL_VALUE)
    void navigate({
      to: '/poe-logs',
      search: {
        p: 1,
        page_size: searchParams.page_size,
        paid_only: true,
        startTime: start.getTime(),
        endTime: end.getTime(),
      },
    })
    queryClient.invalidateQueries({ queryKey: ['poe-logs'] })
    queryClient.invalidateQueries({ queryKey: ['poe-logs-stats'] })
  }, [navigate, queryClient, searchParams.page_size])

  const clearLogsMutation = useMutation({
    mutationFn: clearPoeLogs,
    onSuccess: (result) => {
      if (!result.success) {
        toast.error(t('Failed to clear logs'))
        return
      }

      toast.success(
        t('Cleared {{count}} logs', { count: result.data?.deleted ?? 0 })
      )
      setClearDialogOpen(false)
      queryClient.invalidateQueries({ queryKey: ['poe-logs'] })
      queryClient.invalidateQueries({ queryKey: ['poe-logs-stats'] })
    },
    onError: () => {
      toast.error(t('Failed to clear logs'))
    },
  })

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleApply()
    },
    [handleApply]
  )

  const usageTypeItems = useMemo(
    () => [
      { value: USAGE_TYPE_ALL_VALUE, label: t('All') },
      { value: 'Chat', label: t('Chat') },
      { value: 'API', label: t('API') },
      { value: 'Canvas App', label: t('Canvas App') },
    ],
    [t]
  )
  const usageTypeLabel =
    usageTypeItems.find((item) => item.value === usageType)?.label ?? t('All')
  const hasActiveFilters = Boolean(
    filters.startTime ||
      filters.endTime ||
      filters.channelId ||
      filters.botName ||
      usageType !== USAGE_TYPE_ALL_VALUE ||
      filters.paidOnly === false
  )

  const dateRangeFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={filters.startTime}
        end={filters.endTime}
        onChange={(range) => {
          handleChange('startTime', range.start)
          handleChange('endTime', range.end)
        }}
      />
    </LogsFilterField>
  )
  const channelFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Channel ID')}
        value={filters.channelId || ''}
        onChange={(e) => handleChange('channelId', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const botFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Bot Name')}
        value={filters.botName || ''}
        onChange={(e) => handleChange('botName', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const paidOnlyFilter = (
    <LogsFilterField>
      <label className='flex items-center gap-1.5 text-xs'>
        <Checkbox
          checked={filters.paidOnly ?? true}
          onCheckedChange={(checked) =>
            handleChange('paidOnly', checked === true)
          }
        />
        {t('Paid Only')}
      </label>
    </LogsFilterField>
  )
  const usageTypeFilter = (
    <LogsFilterField>
      <Select
        items={usageTypeItems}
        value={usageType}
        onValueChange={(value) => {
          setUsageType(
            value !== null && isUsageTypeValue(value)
              ? value
              : USAGE_TYPE_ALL_VALUE
          )
        }}
      >
        <SelectTrigger>
          <SelectValue>{usageTypeLabel}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {usageTypeItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
    </LogsFilterField>
  )
  const poeLogsStats = (
    <div className='flex flex-wrap items-center gap-2'>
      <PoeLogsStats />
      <AlertDialog open={clearDialogOpen} onOpenChange={setClearDialogOpen}>
        <AlertDialogTrigger
          render={
            <Button
              type='button'
              variant='destructive'
              disabled={clearLogsMutation.isPending}
            />
          }
        >
          {t('Clear Logs')}
        </AlertDialogTrigger>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Clear All Poe Logs')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This will delete all synced Poe log entries. The next sync will re-fetch them from scratch. This action cannot be undone.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={clearLogsMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              type='button'
              variant='destructive'
              onClick={() => clearLogsMutation.mutate(0)}
              disabled={clearLogsMutation.isPending}
            >
              {t('Clear')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )

  if (!isAdmin) return null

  return (
    <LogsFilterToolbar
      table={props.table}
      stats={poeLogsStats}
      primaryFilters={
        <>
          {dateRangeFilter}
          {channelFilter}
          {botFilter}
          {usageTypeFilter}
          {paidOnlyFilter}
        </>
      }
      mobilePinnedFilters={dateRangeFilter}
      mobileFilters={
        <>
          {channelFilter}
          {botFilter}
          {usageTypeFilter}
          {paidOnlyFilter}
        </>
      }
      mobileFilterCount={
        [filters.channelId, filters.botName, usageType !== USAGE_TYPE_ALL_VALUE, filters.paidOnly === false]
          .filter(Boolean).length
      }
      hasActiveFilters={hasActiveFilters}
      onSearch={handleApply}
      searchLoading={fetchingLogs > 0}
      onReset={handleReset}
    />
  )
}
