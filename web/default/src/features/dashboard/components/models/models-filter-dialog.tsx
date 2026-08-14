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
import { useState } from 'react'
import { Filter, RotateCcw, Calendar, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { type TimeGranularity } from '@/lib/time'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { DateTimePicker } from '@/components/datetime-picker'
import {
  TIME_GRANULARITY_OPTIONS,
  TIME_RANGE_PRESETS,
} from '@/features/dashboard/constants'
import {
  buildDefaultDashboardFilters,
  cleanFilters,
  getDashboardDateRange,
} from '@/features/dashboard/lib'
import type {
  DashboardChartPreferences,
  DashboardFilters,
  TimeRangePresetValue,
} from '@/features/dashboard/types'

interface ModelsFilterProps {
  preferences: DashboardChartPreferences
  filters: DashboardFilters
  onFilterChange: (filters: DashboardFilters) => void
  onReset: () => void
  /** Override the dialog title (defaults to the models-section copy). */
  title?: string
  /** Override the dialog description (defaults to the models-section copy). */
  description?: string
}

const SectionDivider = ({ label }: { label: string }) => (
  <div className='relative'>
    <div className='absolute inset-0 flex items-center'>
      <span className='w-full border-t' />
    </div>
    <div className='relative flex justify-center text-xs uppercase'>
      <span className='bg-background text-muted-foreground px-2'>{label}</span>
    </div>
  </div>
)

// Rolling-window presets keyed by their day count; today/yesterday/month are
// fixed calendar points and are matched separately below.
const ROLLING_DAYS_BY_PRESET: Partial<Record<TimeRangePresetValue, number>> = {
  '1d': 1,
  '7d': 7,
  '14d': 14,
  '29d': 29,
}

const getSelectedRange = (
  filters: DashboardFilters,
  fallbackPreset: TimeRangePresetValue
): TimeRangePresetValue | null => {
  const start = filters.start_timestamp?.getTime()
  const end = filters.end_timestamp?.getTime()
  if (!start || !end) return fallbackPreset

  // Today/yesterday span ~1 day, same as the "1 Day" rolling preset, so they
  // must be checked first against their fixed calendar boundaries (both
  // start and end) before falling back to duration-based matching below.
  for (const preset of ['today', 'yesterday'] as const) {
    const range = getDashboardDateRange(preset)
    if (
      Math.abs(range.start.getTime() - start) < 60_000 &&
      Math.abs(range.end.getTime() - end) < 60_000
    ) {
      return preset
    }
  }

  return (
    TIME_RANGE_PRESETS.find((range) => {
      if (range.value === 'month') {
        const { start: monthStart } = getDashboardDateRange('month')
        return Math.abs(monthStart.getTime() - start) < 60_000
      }

      const days = ROLLING_DAYS_BY_PRESET[range.value]
      if (!days) return false
      return Math.abs(end - start - days * 24 * 60 * 60 * 1000) < 60_000
    })?.value ?? null
  )
}

export function ModelsFilter(props: ModelsFilterProps) {
  const { t } = useTranslation()
  // 使用已缓存的用户数据，避免重复调用 API
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = user?.role && user.role >= 10

  const [open, setOpen] = useState(false)
  const [filters, setFilters] = useState<DashboardFilters>(props.filters)
  const [selectedRange, setSelectedRange] =
    useState<TimeRangePresetValue | null>(() =>
      getSelectedRange(props.filters, props.preferences.defaultTimeRange)
    )

  const resetFiltersFromCurrentFilters = () => {
    setFilters(props.filters)
    setSelectedRange(
      getSelectedRange(props.filters, props.preferences.defaultTimeRange)
    )
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (nextOpen) resetFiltersFromCurrentFilters()
    setOpen(nextOpen)
  }

  const handleApply = () => {
    props.onFilterChange(
      cleanFilters(
        filters as unknown as Record<string, unknown>
      ) as typeof filters
    )
    setOpen(false)
  }

  const handleReset = () => {
    const preset = props.preferences.defaultTimeRange
    const { start, end } = getDashboardDateRange(preset)
    setFilters({
      ...buildDefaultDashboardFilters(props.preferences),
      start_timestamp: start,
      end_timestamp: end,
    })
    setSelectedRange(preset)
    props.onReset()
    setOpen(false)
  }

  const handleChange = (
    field: keyof DashboardFilters,
    value: Date | string | undefined
  ) => {
    setFilters((prev) => ({ ...prev, [field]: value }))
    if (field === 'start_timestamp' || field === 'end_timestamp')
      setSelectedRange(null)
  }

  const handleQuickRange = (preset: TimeRangePresetValue) => {
    const { start, end } = getDashboardDateRange(preset)

    setFilters((prev) => ({
      ...prev,
      start_timestamp: start,
      end_timestamp: end,
    }))
    setSelectedRange(preset)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger render={<Button variant='outline' size='sm' />}>
        <Filter className='mr-2 h-4 w-4' />
        {t('Filter')}
      </DialogTrigger>
      <DialogContent className='flex max-h-[calc(100dvh-2rem)] flex-col max-sm:h-dvh max-sm:w-screen max-sm:max-w-none max-sm:rounded-none max-sm:p-4 sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {props.title ?? t('Filter Dashboard Models')}
          </DialogTitle>
          <DialogDescription>
            {props.description ??
              t(
                'Set filters to customize your dashboard statistics and charts.'
              )}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className='flex-1 pr-3 sm:pr-4'>
          <div className='grid gap-3 py-3 sm:gap-4 sm:py-4'>
            {/* Quick time range selection */}
            <div className='grid gap-2'>
              <Label className='flex items-center gap-2'>
                <Calendar className='h-4 w-4' />
                {t('Quick Range')}
              </Label>
              <div className='grid grid-cols-3 gap-2 sm:grid-cols-4'>
                {TIME_RANGE_PRESETS.map((range) => (
                  <Button
                    key={range.value}
                    type='button'
                    size='sm'
                    variant={
                      selectedRange === range.value ? 'default' : 'outline'
                    }
                    onClick={() => handleQuickRange(range.value)}
                    className={cn(
                      'w-full',
                      selectedRange === range.value &&
                        'ring-ring ring-2 ring-offset-2'
                    )}
                  >
                    {t(range.label)}
                  </Button>
                ))}
              </div>
            </div>

            <SectionDivider label={t('Custom Time Range')} />

            {/* Custom time range */}
            <div className='grid gap-3 sm:gap-4'>
              <div className='grid gap-2'>
                <Label htmlFor='start_timestamp'>{t('Start Time')}</Label>
                <DateTimePicker
                  value={filters.start_timestamp}
                  onChange={(date) =>
                    handleChange('start_timestamp', date || undefined)
                  }
                  placeholder={t('Select start time')}
                />
              </div>

              <div className='grid gap-2'>
                <Label htmlFor='end_timestamp'>{t('End Time')}</Label>
                <DateTimePicker
                  value={filters.end_timestamp}
                  onChange={(date) =>
                    handleChange('end_timestamp', date || undefined)
                  }
                  placeholder={t('Select end time')}
                />
              </div>
            </div>

            <SectionDivider label={t('Chart Settings')} />

            <div className='grid gap-2'>
              <Label htmlFor='time_granularity'>{t('Time Granularity')}</Label>
              <Select
                items={[
                  ...TIME_GRANULARITY_OPTIONS.map((option) => ({
                    value: option.value,
                    label: t(option.label),
                  })),
                ]}
                value={filters.time_granularity}
                onValueChange={(value) =>
                  handleChange('time_granularity', value as TimeGranularity)
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder={t('Select time granularity')} />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {TIME_GRANULARITY_OPTIONS.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {t(option.label)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </div>

            {/* Admin-only fields */}
            {isAdmin && (
              <>
                <SectionDivider label={t('Admin Only')} />

                <div className='grid gap-2'>
                  <Label htmlFor='username'>{t('Username')}</Label>
                  <Input
                    id='username'
                    placeholder={t('Filter by username')}
                    value={filters.username}
                    onChange={(e) => handleChange('username', e.target.value)}
                  />
                </div>
              </>
            )}
          </div>
        </ScrollArea>

        <DialogFooter className='grid grid-cols-2 gap-2 sm:flex'>
          <Button onClick={handleReset} variant='outline' type='button'>
            <RotateCcw className='mr-2 h-4 w-4' />
            {t('Reset')}
          </Button>
          <Button onClick={handleApply} type='submit'>
            <Search className='mr-2 h-4 w-4' />
            {t('Apply Filters')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
