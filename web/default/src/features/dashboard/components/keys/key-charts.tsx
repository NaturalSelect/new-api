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

type KeyMetric =
  | 'total_tokens'
  | 'count'
  | 'input_tokens'
  | 'output_tokens'
  | 'cache_read_tokens'
  | 'cache_write_tokens'

const KEY_METRIC_OPTIONS: { value: KeyMetric; labelKey: string }[] = [
  { value: 'total_tokens', labelKey: 'Total Tokens' },
  { value: 'count', labelKey: 'Call Count' },
  { value: 'input_tokens', labelKey: 'Input (Cache Miss)' },
  { value: 'output_tokens', labelKey: 'Output Tokens' },
  { value: 'cache_read_tokens', labelKey: 'Input (Cache Hit)' },
  { value: 'cache_write_tokens', labelKey: 'Cache Write' },
]

// Max items visible without scrolling
const CHART_VISIBLE_ITEMS = 20

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

interface KeyChartsProps {
  filters?: DashboardFilters
}

// Max models shown as distinct series before the rest fold into "Other"
const MAX_KEY_MODELS = 20

type KeyRankLabels = {
  title: string
  otherLabel: string
  totalLabel: string
}

type KeyAggregate = {
  token_id: number
  token_name: string
  total: number
  models: Map<string, number>
}

// getMetricValue resolves the display value for a metric tab. input_tokens shows
// the cache-miss portion (input - cache_read), mirroring the Token Distribution
// chart, so the four category tabs (input/output/cache_read/cache_write) sum
// exactly to total_tokens instead of double-counting cache_read.
function getMetricValue(item: KeyDistributionDataItem, metric: KeyMetric) {
  if (metric === 'input_tokens') {
    const input = Number(item.input_tokens) || 0
    const cacheRead = Number(item.cache_read_tokens) || 0
    return Math.max(input - cacheRead, 0)
  }
  return Number(item[metric]) || 0
}

function buildKeyRankSpec(
  data: KeyDistributionDataItem[],
  metric: KeyMetric,
  labels: KeyRankLabels
) {
  const formatInt = (value: number) =>
    Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value)

  // Global per-model totals (across all keys) keep the top-N model set —
  // and therefore the legend/color set — stable across every key row.
  const modelTotals = new Map<string, number>()
  for (const item of data) {
    const model = item.model_name || 'Unknown'
    modelTotals.set(
      model,
      (modelTotals.get(model) || 0) + getMetricValue(item, metric)
    )
  }
  const rankedModels = Array.from(modelTotals.entries()).sort(
    (a, b) => b[1] - a[1]
  )
  const topModels = new Set(
    rankedModels.slice(0, MAX_KEY_MODELS).map(([model]) => model)
  )

  // Aggregate the selected metric per (key, model bucket); non-top models fold
  // into the "Other" series so the legend stays readable.
  const keyMap = new Map<number, KeyAggregate>()
  for (const item of data) {
    const value = getMetricValue(item, metric)
    const model = item.model_name || 'Unknown'
    const bucket = topModels.has(model) ? model : labels.otherLabel
    let agg = keyMap.get(item.token_id)
    if (!agg) {
      agg = {
        token_id: item.token_id,
        token_name: item.token_name ?? '',
        total: 0,
        models: new Map(),
      }
      keyMap.set(item.token_id, agg)
    }
    // XX: prefer first non-empty token_name for the key label
    if (!agg.token_name && item.token_name) agg.token_name = item.token_name
    agg.total += value
    agg.models.set(bucket, (agg.models.get(bucket) || 0) + value)
  }

  // Order keys by their total of the selected metric (descending); the band
  // axis follows data order, so highest-usage keys render first.
  const sortedKeys = Array.from(keyMap.values()).sort(
    (a, b) => b.total - a.total
  )

  // token_name has no uniqueness constraint (only the key value itself is
  // unique), so two distinct keys can share a display name — e.g. many users
  // naming a key "default". The band axis groups bars by this label, so a
  // collision would silently merge two different keys into one bar. Only
  // disambiguate names that actually collide, keeping the common case (all
  // names unique) clean.
  const nameCounts = new Map<string, number>()
  for (const key of sortedKeys) {
    const label = key.token_name ? key.token_name : `Key #${key.token_id}`
    nameCounts.set(label, (nameCounts.get(label) || 0) + 1)
  }

  const values: Array<{ Key: string; Model: string; Value: number }> = []
  for (const key of sortedKeys) {
    const baseLabel = key.token_name ? key.token_name : `Key #${key.token_id}`
    const label =
      (nameCounts.get(baseLabel) || 0) > 1
        ? `${baseLabel} (#${key.token_id})`
        : baseLabel
    for (const [model, value] of key.models) {
      values.push({ Key: label, Model: model, Value: value })
    }
  }

  const uniqueKeyCount = sortedKeys.length
  const needsScroll = uniqueKeyCount > CHART_VISIBLE_ITEMS
  const endRatio = needsScroll ? CHART_VISIBLE_ITEMS / uniqueKeyCount : 1

  return {
    type: 'bar',
    direction: 'horizontal',
    data: [{ id: 'keyRankData', values }],
    xField: 'Value',
    yField: 'Key',
    seriesField: 'Model',
    stack: true,
    title: { visible: true, text: labels.title },
    legends: { visible: true, selectMode: 'single' },
    bar: { state: { hover: { stroke: '#000', lineWidth: 1 } } },
    totalLabel: {
      visible: true,
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
            key: (datum: Record<string, unknown>) => datum?.Model,
            value: (datum: Record<string, unknown>) =>
              formatInt(Number(datum?.Value) || 0),
          },
        ],
      },
      dimension: {
        content: [
          {
            key: (datum: Record<string, unknown>) => datum?.Model,
            value: (datum: Record<string, unknown>) =>
              Number(datum?.Value) || 0,
          },
        ],
        updateContent: (
          array: Array<{ key: string; value: string | number }>
        ) => {
          array.sort((a, b) => (Number(b.value) || 0) - (Number(a.value) || 0))
          let sum = 0
          for (let i = 0; i < array.length; i++) {
            const v = Number(array[i].value) || 0
            sum += v
            array[i].value = formatInt(v)
          }
          array.unshift({ key: labels.totalLabel, value: formatInt(sum) })
          return array
        },
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

  const rows = useMemo(() => (isLoading ? [] : (data ?? [])), [data, isLoading])

  const spec = useMemo(
    () =>
      buildKeyRankSpec(rows, activeMetric, {
        title: t('Key Usage Ranking'),
        otherLabel: t('Other'),
        totalLabel: t('Total:'),
      }),
    [rows, activeMetric, t]
  )

  const chartKey = [
    activeMetric,
    isLoading ? 'loading' : 'ready',
    rows.length,
    resolvedTheme,
    customization.preset,
  ].join('-')

  const isEmpty = !isLoading && !isError && rows.length === 0

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
