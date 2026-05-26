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
} from '@/features/dashboard/types'

interface ModelsFilterProps {
  preferences: DashboardChartPreferences
  filters: DashboardFilters
  onFilterChange: (filters: DashboardFilters) => void
  onReset: () => void
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

const getSelectedRange = (
  filters: DashboardFilters,
  fallbackDays: number
): number | null => {
  const start = filters.start_timestamp?.getTime()
  const end = filters.end_timestamp?.getTime()
  if (!start || !end) return fallbackDays

  return (
    TIME_RANGE_PRESETS.find((range) => {
      if (range.days === 0) {
        const { start: monthStart } = getDashboardDateRange(0)
        return Math.abs(monthStart.getTime() - start) < 60_000
      }

      return Math.abs(end - start - range.days * 24 * 60 * 60 * 1000) < 60_000
    })?.days ?? null
  )
}

export function ModelsFilter(props: ModelsFilterProps) {
  const { t } = useTranslation()
  // 使用已缓存的用户数据，避免重复调用 API
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = user?.role && user.role >= 10

  const [open, setOpen] = useState(false)
  const [filters, setFilters] = useState<DashboardFilters>(props.filters)
  const [selectedRange, setSelectedRange] = useState<number | null>(() =>
    getSelectedRange(props.filters, props.preferences.defaultTimeRangeDays)
  )

  const resetFiltersFromCurrentFilters = () => {
    setFilters(props.filters)
    setSelectedRange(
      getSelectedRange(props.filters, props.preferences.defaultTimeRangeDays)
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
    const days = props.preferences.defaultTimeRangeDays
    const { start, end } = getDashboardDateRange(days)
    setFilters({
      ...buildDefaultDashboardFilters(props.preferences),
      start_timestamp: start,
      end_timestamp: end,
    })
    setSelectedRange(days)
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

  const handleQuickRange = (days: number) => {
    const { start, end } = getDashboardDateRange(days)

    setFilters((prev) => ({
      ...prev,
      start_timestamp: start,
      end_timestamp: end,
    }))
    setSelectedRange(days)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogTrigger render={<Button variant='outline' size='sm' />}>
        <Filter className='mr-2 h-4 w-4' />
        {t('Filter')}
      </DialogTrigger>
      <DialogContent className='flex max-h-[calc(100dvh-2rem)] flex-col max-sm:h-dvh max-sm:w-screen max-sm:max-w-none max-sm:rounded-none max-sm:p-4 sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>{t('Filter Dashboard Models')}</DialogTitle>
          <DialogDescription>
            {t(
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
              <div className='grid grid-cols-2 gap-2 sm:flex'>
                {TIME_RANGE_PRESETS.map((range) => (
                  <Button
                    key={range.days}
                    type='button'
                    size='sm'
                    variant={
                      selectedRange === range.days ? 'default' : 'outline'
                    }
                    onClick={() => handleQuickRange(range.days)}
                    className={cn(
                      'flex-1',
                      selectedRange === range.days &&
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
