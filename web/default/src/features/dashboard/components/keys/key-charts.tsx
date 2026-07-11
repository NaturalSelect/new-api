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
import { useEffect, useMemo, useState, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { AlertCircle, Key } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { computeTimeRange } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useTheme } from '@/context/theme-provider'
import { useIsAdmin } from '@/hooks/use-admin'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { getKeyDistribution } from '@/features/dashboard/api'
import { buildQueryParams, getDefaultDays } from '@/features/dashboard/lib'
import type {
  DashboardFilters,
  KeyDistributionDataItem,
} from '@/features/dashboard/types'

type KeyMetric = 'total_tokens' | 'count' | 'input_tokens' | 'output_tokens'

const KEY_METRIC_OPTIONS: { value: KeyMetric; labelKey: string }[] = [
  { value: 'total_tokens', labelKey: 'Total Tokens' },
  { value: 'count', labelKey: 'Call Count' },
  { value: 'input_tokens', labelKey: 'Input Tokens' },
  { value: 'output_tokens', labelKey: 'Output Tokens' },
]

// Max items visible without scrolling
const CHART_VISIBLE_ITEMS = 20

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

interface KeyChartsProps {
  filters?: DashboardFilters
}

type AggregatedKeyItem = {
  token_id: number
  token_name: string
  input_tokens: number
  output_tokens: number
  total_tokens: number
  count: number
}

function aggregateByKey(data: KeyDistributionDataItem[]): AggregatedKeyItem[] {
  const map = new Map<number, AggregatedKeyItem>()
  for (const item of data) {
    const input = Number(item.input_tokens) || 0
    const output = Number(item.output_tokens) || 0
    const total = Number(item.total_tokens) || 0
    const count = Number(item.count) || 0
    const existing = map.get(item.token_id)
    if (existing) {
      // XX: prefer first non-empty token_name for the key label
      if (!existing.token_name && item.token_name) {
        existing.token_name = item.token_name
      }
      existing.input_tokens += input
      existing.output_tokens += output
      existing.total_tokens += total
      existing.count += count
    } else {
      map.set(item.token_id, {
        token_id: item.token_id,
        token_name: item.token_name ?? '',
        input_tokens: input,
        output_tokens: output,
        total_tokens: total,
        count,
      })
    }
  }
  return Array.from(map.values())
}

function buildKeyRankSpec(
  aggregated: AggregatedKeyItem[],
  metric: KeyMetric,
  title: string
) {
  const formatInt = (value: number) =>
    Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value)

  const sorted = [...aggregated].sort((a, b) => b[metric] - a[metric])

  const values = sorted.map((item) => ({
    Key: item.token_name ? item.token_name : `Key #${item.token_id}`,
    Value: item[metric],
  }))

  const needsScroll = values.length > CHART_VISIBLE_ITEMS
  const endRatio = needsScroll ? CHART_VISIBLE_ITEMS / values.length : 1

  return {
    type: 'bar',
    direction: 'horizontal',
    data: [{ id: 'keyRankData', values }],
    xField: 'Value',
    yField: 'Key',
    seriesField: 'Key',
    title: { visible: true, text: title },
    bar: { state: { hover: { stroke: '#000', lineWidth: 1 } } },
    label: {
      visible: true,
      position: 'outside',
      formatMethod: (value: number) => formatInt(value),
      style: { fontSize: 11 },
    },
    axes: [
      { orient: 'left', type: 'band' },
      { orient: 'bottom', type: 'linear', visible: false },
    ],
    tooltip: {
      mark: {
        content: [
          {
            key: (datum: Record<string, unknown>) => datum?.Key,
            value: (datum: Record<string, unknown>) =>
              formatInt(Number(datum?.Value) || 0),
          },
        ],
      },
    },
    ...(needsScroll
      ? {
          scrollBar: [
            { orient: 'right', start: 0, end: endRatio, filterMode: 'axis' },
          ],
        }
      : {}),
    background: { fill: 'transparent' },
    animation: true,
  }
}

export function KeyCharts(props: KeyChartsProps) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const [activeMetric, setActiveMetric] = useState<KeyMetric>('total_tokens')
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)
  const isAdmin = useIsAdmin()

  const timeRange = computeTimeRange(
    getDefaultDays(props.filters?.time_granularity),
    props.filters?.start_timestamp,
    props.filters?.end_timestamp
  )
  // XX: the self endpoint always resolves the acting user from the auth
  // session, so only forward the username filter on admin requests.
  const username = isAdmin ? props.filters?.username : undefined

  const { data, isLoading, isError, refetch } = useQuery({
    queryKey: ['dashboard', 'key-distribution', isAdmin, timeRange, username],
    queryFn: () =>
      getKeyDistribution(
        buildQueryParams(timeRange, {
          time_granularity: props.filters?.time_granularity,
          username,
        }),
        isAdmin
      ),
    select: (res) => (res.success ? res.data : []),
  })

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }
    updateTheme()
  }, [resolvedTheme])

  const aggregated = useMemo(
    () => aggregateByKey(isLoading ? [] : (data ?? [])),
    [data, isLoading]
  )

  const spec = useMemo(
    () => buildKeyRankSpec(aggregated, activeMetric, t('Key Usage Ranking')),
    [aggregated, activeMetric, t]
  )

  const chartKey = [
    activeMetric,
    isLoading ? 'loading' : 'ready',
    aggregated.length,
    resolvedTheme,
    customization.preset,
  ].join('-')

  const isEmpty = !isLoading && !isError && aggregated.length === 0

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex w-full flex-col gap-1.5 border-b px-3 py-2 sm:gap-3 sm:px-5 sm:py-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='flex items-center gap-2'>
          <Key className='text-muted-foreground/60 size-4' />
          <div className='text-sm font-semibold'>{t('Key Statistics')}</div>
        </div>
        <div className='bg-muted/60 inline-flex h-7 w-full overflow-x-auto rounded-lg border p-0.5 sm:h-8 sm:w-auto'>
          {KEY_METRIC_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              type='button'
              onClick={() => setActiveMetric(opt.value)}
              className={`shrink-0 rounded-md px-3 text-xs font-medium transition-colors ${
                activeMetric === opt.value
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:text-foreground'
              }`}
            >
              {t(opt.labelKey)}
            </button>
          ))}
        </div>
      </div>

      <div className='h-[300px] p-1.5 sm:h-96 sm:p-2'>
        {isLoading ? (
          <Skeleton className='h-full w-full' />
        ) : isError ? (
          <div className='flex h-full flex-col items-center justify-center gap-3'>
            <AlertCircle className='text-muted-foreground size-6' />
            <div className='text-center'>
              <p className='text-sm font-medium'>{t('Loading failed')}</p>
              <p className='text-muted-foreground text-xs'>
                {t('Failed to load')}
              </p>
            </div>
            <Button variant='outline' size='sm' onClick={() => refetch()}>
              {t('Retry')}
            </Button>
          </div>
        ) : isEmpty ? (
          <div className='flex h-full items-center justify-center'>
            <p className='text-muted-foreground text-sm'>
              {t('No Key Usage Found')}
            </p>
          </div>
        ) : (
          themeReady && (
            <VChart
              key={chartKey}
              spec={{
                ...spec,
                theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                background: 'transparent',
              }}
              option={VCHART_OPTION}
            />
          )
        )}
      </div>
    </div>
  )
}
