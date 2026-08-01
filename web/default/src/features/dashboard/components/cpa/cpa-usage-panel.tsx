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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound, RotateCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { getCPAUsage, refreshCPAUsage } from '@/features/dashboard/api'
import type { CPAUsageItem } from '@/features/dashboard/types'
import { PanelWrapper } from '../ui/panel-wrapper'
import { CPAUsageBar } from './cpa-usage-bar'

const CPA_USAGE_QUERY_KEY = ['dashboard', 'cpa-usage']

function groupByType(items: CPAUsageItem[]) {
  const groups = new Map<string, CPAUsageItem[]>()
  for (const item of items) {
    const key = item.type || 'unknown'
    const list = groups.get(key) ?? []
    list.push(item)
    groups.set(key, list)
  }
  return groups
}

function CredentialRow(props: { item: CPAUsageItem }) {
  const { t } = useTranslation()
  const observedDisplay = dayjs(props.item.observed_at).isValid()
    ? dayjs(props.item.observed_at).fromNow()
    : null

  return (
    <div className='hover:bg-muted/40 flex flex-col gap-3 px-3 py-3 transition-colors sm:px-5 sm:flex-row sm:items-center sm:gap-6'>
      <div className='flex min-w-0 flex-1 items-center gap-2'>
        <span className='truncate text-sm font-medium'>
          {props.item.name}
        </span>
      </div>
      <div className='grid flex-1 grid-cols-1 gap-3 sm:grid-cols-2'>
        {props.item.usage_7d && (
          <CPAUsageBar
            label={t('7-day')}
            percent={props.item.usage_7d.percent}
            resetAt={props.item.usage_7d.reset_at}
          />
        )}
        {props.item.usage_5h && (
          <CPAUsageBar
            label={t('5-hour')}
            percent={props.item.usage_5h.percent}
            resetAt={props.item.usage_5h.reset_at}
          />
        )}
      </div>
      {observedDisplay && (
        <span className='text-muted-foreground/70 shrink-0 text-xs'>
          {t('Observed')} {observedDisplay}
        </span>
      )}
    </div>
  )
}

export function CPAUsagePanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading } = useQuery({
    queryKey: CPA_USAGE_QUERY_KEY,
    queryFn: getCPAUsage,
    select: (res) =>
      res.success
        ? res.data
        : { usage: [] as CPAUsageItem[], updated_at: 0, configured: false },
    staleTime: 60_000,
  })

  const refreshMutation = useMutation({
    mutationFn: refreshCPAUsage,
    onSuccess: (res) => {
      if (res.success && res.data) {
        queryClient.setQueryData(CPA_USAGE_QUERY_KEY, res)
      }
    },
  })

  const usage = data?.usage ?? []
  const updatedAt = data?.updated_at ?? 0
  const configured = data?.configured ?? false
  const groups = useMemo(() => groupByType(usage), [usage])
  const updatedAtDisplay = updatedAt ? dayjs.unix(updatedAt).fromNow() : null

  const emptyMessage = configured
    ? t('No usage data available. Data appears after CPA receives upstream responses.')
    : t('CPA service is not configured. Go to Settings to set it up.')

  return (
    <PanelWrapper
      title={
        <span className='flex items-center gap-2'>
          <KeyRound className='text-muted-foreground/60 size-4' />
          {t('CPA Credential Usage')}
        </span>
      }
      description={
        updatedAtDisplay
          ? `${t('Last synced')} ${updatedAtDisplay}`
          : undefined
      }
      loading={isLoading}
      empty={usage.length === 0}
      emptyMessage={emptyMessage}
      contentClassName='p-0'
      headerActions={
        <Button
          variant='ghost'
          size='sm'
          onClick={() => refreshMutation.mutate()}
          disabled={refreshMutation.isPending}
          className='size-7 p-0'
        >
          <RotateCw
            className={cn(
              'size-3.5',
              refreshMutation.isPending && 'animate-spin'
            )}
            aria-label={t('Refresh')}
          />
        </Button>
      }
    >
      <div>
        {Array.from(groups.entries()).map(([type, items], groupIdx) => (
          <div key={type}>
            <div className='bg-muted/30 border-border/60 flex items-center gap-2 border-b px-3 py-2 sm:px-5'>
              <Badge variant='outline' className='capitalize'>
                {t(type.charAt(0).toUpperCase() + type.slice(1))}
              </Badge>
              <span className='text-muted-foreground/40 font-mono text-xs tabular-nums'>
                {items.length}
              </span>
            </div>
            {items.map((item, itemIdx) => (
              <div
                key={item.id}
                className={cn(
                  itemIdx < items.length - 1 && 'border-border/40 border-b',
                  groupIdx < groups.size - 1 &&
                    itemIdx === items.length - 1 &&
                    'border-border/60 border-b'
                )}
              >
                <CredentialRow item={item} />
              </div>
            ))}
          </div>
        ))}
      </div>
    </PanelWrapper>
  )
}
