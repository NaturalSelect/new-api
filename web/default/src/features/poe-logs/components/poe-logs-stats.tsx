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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useIsAdmin } from '@/hooks/use-admin'
import { Skeleton } from '@/components/ui/skeleton'
import { getPoeLogStats } from '../api'

const route = getRouteApi('/_authenticated/poe-logs/')

const DEFAULT_STATS = {
  total_points: 0,
  total_usd: '',
  count: 0,
}

function toApiTimestamp(value?: number): number | undefined {
  return value ? Math.floor(value / 1000) : undefined
}

function toChannelId(value?: string): number | undefined {
  if (!value) return undefined
  const channelId = Number(value)
  return Number.isFinite(channelId) && channelId > 0 ? channelId : undefined
}

function StatBadge(props: {
  label: string
  value: string | number
  accent: string
}) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex h-7 items-center gap-2 rounded-md border px-2.5 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 rounded-full', props.accent)} />
      <span className='text-muted-foreground'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
    </span>
  )
}

export function PoeLogsStats() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const searchParams = route.useSearch()

  const { data: stats, isLoading } = useQuery({
    queryKey: ['poe-logs-stats', searchParams],
    queryFn: async () => {
      const result = await getPoeLogStats({
        channel_id: toChannelId(searchParams.channel_id),
        start_timestamp: toApiTimestamp(searchParams.startTime),
        end_timestamp: toApiTimestamp(searchParams.endTime),
      })
      return result.success ? result.data || DEFAULT_STATS : DEFAULT_STATS
    },
    enabled: isAdmin,
    placeholderData: (previousData) => previousData,
  })

  if (isLoading) {
    return (
      <div className='flex items-center gap-2'>
        <Skeleton className='h-7 w-[150px] rounded-md' />
        <Skeleton className='h-7 w-[100px] rounded-md' />
      </div>
    )
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <StatBadge
        label={t('Total Points')}
        value={formatNumber(stats?.total_points || 0)}
        accent='bg-sky-500/70'
      />
      <StatBadge
        label={t('Count')}
        value={formatNumber(stats?.count || 0)}
        accent='bg-slate-400/70'
      />
    </div>
  )
}
