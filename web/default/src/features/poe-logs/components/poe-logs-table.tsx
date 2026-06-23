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
import { useEffect, useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  getCoreRowModel,
  getPaginationRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatNumber, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useIsAdmin } from '@/hooks/use-admin'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { DataTableColumnHeader, DataTablePage } from '@/components/data-table'
import { getAllPoeLogs, triggerPoeLogSync } from '../api'
import type { GetPoeLogsParams, PoeLog } from '../types'
import { PoeLogsFilterBar } from './poe-logs-filter-bar'

const route = getRouteApi('/_authenticated/poe-logs/')

function toApiTimestamp(value?: number): number | undefined {
  return value ? Math.floor(value / 1000) : undefined
}

function toChannelId(value?: string): number | undefined {
  if (!value) return undefined
  const channelId = Number(value)
  return Number.isFinite(channelId) && channelId > 0 ? channelId : undefined
}

function formatMicroseconds(value: number): string {
  return formatTimestampToDate(Math.floor(value / 1000), 'milliseconds')
}

function formatSeconds(value: number): string {
  return formatTimestampToDate(value, 'seconds')
}

function parseBreakdown(value: string): Array<[string, string]> {
  if (!value) return []
  try {
    const parsed = JSON.parse(value) as unknown
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return []
    return Object.entries(parsed as Record<string, unknown>).map(([key, item]) => [
      key,
      String(item),
    ])
  } catch {
    return []
  }
}

function buildApiParams(config: {
  page: number
  pageSize: number
  searchParams: Record<string, unknown>
}): GetPoeLogsParams {
  const channelId =
    typeof config.searchParams.channel_id === 'string'
      ? toChannelId(config.searchParams.channel_id)
      : undefined

  return {
    p: config.page,
    page_size: config.pageSize,
    channel_id: channelId,
    bot_name:
      typeof config.searchParams.bot_name === 'string'
        ? config.searchParams.bot_name
        : undefined,
    usage_type:
      typeof config.searchParams.usage_type === 'string'
        ? config.searchParams.usage_type
        : undefined,
    paid_only:
      config.searchParams.paid_only !== false
        ? true
        : false,
    start_timestamp:
      typeof config.searchParams.startTime === 'number'
        ? toApiTimestamp(config.searchParams.startTime)
        : undefined,
    end_timestamp:
      typeof config.searchParams.endTime === 'number'
        ? toApiTimestamp(config.searchParams.endTime)
        : undefined,
  }
}

function BreakdownCell(props: { value: string }) {
  const { t } = useTranslation()
  const entries = parseBreakdown(props.value)

  if (entries.length === 0) {
    return <span className='text-muted-foreground text-xs'>-</span>
  }

  return (
    <Popover>
      <PopoverTrigger
        render={
          <Button variant='ghost' size='sm' className='h-7 px-2 text-xs' />
        }
      >
        {t('View')}
      </PopoverTrigger>
      <PopoverContent align='end' className='w-80'>
        <div className='space-y-2'>
          <div className='text-sm font-medium'>{t('Breakdown')}</div>
          <div className='space-y-1.5'>
            {entries.map(([key, value]) => (
              <div
                key={key}
                className='flex items-start justify-between gap-3 text-xs'
              >
                <span className='text-muted-foreground shrink-0'>{key}</span>
                <span className='text-right font-mono break-all'>{value}</span>
              </div>
            ))}
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}

function usePoeLogsColumns(opts: {
  onSync: (channelId: number) => void
  syncingChannelId?: number
}): ColumnDef<PoeLog>[] {
  const { t } = useTranslation()

  return useMemo(
    () => [
      {
        accessorKey: 'creation_time',
        meta: { label: t('Time'), mobileTitle: true },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Time')} />
        ),
        cell: ({ row }) => (
          <div className='min-w-[150px] font-mono text-xs tabular-nums'>
            {formatMicroseconds(row.original.creation_time)}
          </div>
        ),
      },
      {
        accessorKey: 'channel_id',
        meta: { label: t('Channel') },
        header: t('Channel'),
        cell: ({ row }) => {
          const id = row.original.channel_id
          const name = row.original.channel_name
          return (
            <span className='font-mono text-xs'>
              {name ? `${name} (#${id})` : `#${id}`}
            </span>
          )
        },
      },
      {
        accessorKey: 'bot_name',
        meta: { label: t('Bot Name'), mobileBadge: true },
        header: t('Bot Name'),
        cell: ({ row }) => (
          <Badge variant='secondary' className='font-mono'>
            {row.original.bot_name || '-'}
          </Badge>
        ),
      },
      {
        accessorKey: 'usage_type',
        meta: { label: t('Usage Type') },
        header: t('Usage Type'),
        cell: ({ row }) => row.original.usage_type || '-',
      },
      {
        accessorKey: 'prompt_tokens',
        meta: { label: 'Tokens' },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title='Tokens' />
        ),
        cell: ({ row }) => {
          const prompt = row.original.prompt_tokens || 0
          const completion = row.original.completion_tokens || 0
          const cache = row.original.cache_tokens || 0
          const cacheWrite = row.original.cache_write_tokens || 0
          if (prompt === 0 && completion === 0 && cache === 0 && cacheWrite === 0) {
            return <span className='text-muted-foreground text-xs'>-</span>
          }
          return (
            <div className='flex flex-col gap-0.5'>
              <span className='font-mono text-xs font-medium tabular-nums'>
                {prompt.toLocaleString()} / {completion.toLocaleString()}
              </span>
              {cacheWrite > 0 && (
                <span className='text-muted-foreground/60 text-[11px]'>
                  {t('Cache')}↑ {cacheWrite.toLocaleString()}
                </span>
              )}
              {cache > 0 && (
                <span className='text-muted-foreground/60 text-[11px]'>
                  {t('Cache')}↓ {cache.toLocaleString()}
                </span>
              )}
            </div>
          )
        },
      },
      {
        accessorKey: 'cost_points',
        meta: { label: t('Cost Points') },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Cost Points')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {formatNumber(row.original.cost_points)}
          </span>
        ),
      },
      {
        accessorKey: 'cost_usd',
        meta: { label: t('Cost USD') },
        header: t('Cost USD'),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {row.original.cost_usd || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'cost_breakdown',
        meta: { label: t('Breakdown') },
        header: t('Breakdown'),
        cell: ({ row }) => <BreakdownCell value={row.original.cost_breakdown} />,
        enableSorting: false,
      },
      {
        accessorKey: 'synced_at',
        meta: { label: t('Synced At') },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Synced At')} />
        ),
        cell: ({ row }) => (
          <div className='min-w-[150px] font-mono text-xs tabular-nums'>
            {formatSeconds(row.original.synced_at)}
          </div>
        ),
      },
      {
        id: 'actions',
        meta: { label: t('Actions') },
        enableHiding: false,
        enableSorting: false,
        cell: ({ row }) => {
          const channelId = row.original.channel_id
          const isSyncing = opts.syncingChannelId === channelId
          return (
            <Button
              variant='outline'
              size='sm'
              className='h-7 gap-1.5 px-2 text-xs'
              disabled={isSyncing}
              onClick={() => opts.onSync(channelId)}
            >
              {isSyncing && <Loader2 className='size-3 animate-spin' />}
              {t('Sync')}
            </Button>
          )
        },
      },
    ],
    [opts, t]
  )
}

export function PoeLogsTable() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const searchParams = route.useSearch()
  const queryClient = useQueryClient()

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search: searchParams,
      navigate: route.useNavigate(),
      pagination: {
        pageKey: 'p',
        pageSizeKey: 'page_size',
        defaultPage: 1,
        defaultPageSize: isMobile ? 20 : 100,
      },
      globalFilter: { enabled: false },
    })

  const syncMutation = useMutation({
    mutationFn: triggerPoeLogSync,
    onSuccess: (result) => {
      if (result.success) {
        toast.success(t('Sync completed'))
        queryClient.invalidateQueries({ queryKey: ['poe-logs'] })
        queryClient.invalidateQueries({ queryKey: ['poe-logs-stats'] })
      } else {
        toast.error(t('Sync failed'))
      }
    },
    onError: () => {
      toast.error(t('Sync failed'))
    },
  })

  const columns = usePoeLogsColumns({
    onSync: (channelId) => syncMutation.mutate(channelId),
    syncingChannelId: syncMutation.isPending
      ? syncMutation.variables
      : undefined,
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['poe-logs', searchParams],
    queryFn: async () => {
      const result = await getAllPoeLogs(
        buildApiParams({
          page: pagination.pageIndex + 1,
          pageSize: pagination.pageSize,
          searchParams,
        })
      )

      if (!result.success) {
        toast.error(result.message || t('Failed to load logs'))
        return { items: [], total: 0, page: 1, page_size: pagination.pageSize }
      }

      return (
        result.data || {
          items: [],
          total: 0,
          page: 1,
          page_size: pagination.pageSize,
        }
      )
    },
    enabled: isAdmin,
    placeholderData: (previousData) => previousData,
  })

  const logs = data?.items || []
  const table = useReactTable({
    data: logs,
    columns,
    state: { pagination },
    enableRowSelection: false,
    onPaginationChange,
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    manualPagination: true,
    pageCount: Math.ceil((data?.total || 0) / pagination.pageSize),
  })

  const pageCount = table.getPageCount()
  useEffect(() => {
    ensurePageInRange(pageCount)
  }, [ensurePageInRange, pageCount])

  if (!isAdmin) return null

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No Poe Logs Found')}
      emptyDescription={t(
        'No Poe usage logs available. Logs will appear here after syncing Poe channels.'
      )}
      skeletonKeyPrefix='poe-log-skeleton'
      tableClassName={cn(
        'overflow-x-auto',
        '[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
      )}
      tableHeaderClassName='bg-muted/30 sticky top-0 z-10'
      toolbar={<PoeLogsFilterBar table={table} />}
    />
  )
}
