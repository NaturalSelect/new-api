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
import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  getCoreRowModel,
  getPaginationRowModel,
  useReactTable,
  type ColumnDef,
} from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useIsAdmin } from '@/hooks/use-admin'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { DataTableColumnHeader, DataTablePage } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { getAllWarningLogs } from '../api'
import type { GetWarningLogsParams, WarningLog } from '../types'
import { WarningLogDetailsDialog } from './warning-log-details-dialog'
import { WarningLogsFilterBar } from './warning-logs-filter-bar'

const route = getRouteApi('/_authenticated/warning-logs/')

function toApiTimestamp(value?: number): number | undefined {
  return value ? Math.floor(value / 1000) : undefined
}

function toId(value?: string): number | undefined {
  if (!value) return undefined
  const id = Number(value)
  return Number.isFinite(id) && id > 0 ? id : undefined
}

function buildApiParams(config: {
  page: number
  pageSize: number
  searchParams: Record<string, unknown>
}): GetWarningLogsParams {
  const search = config.searchParams

  return {
    p: config.page,
    page_size: config.pageSize,
    user_id: typeof search.user_id === 'string' ? toId(search.user_id) : undefined,
    username: typeof search.username === 'string' ? search.username : undefined,
    token_name:
      typeof search.token_name === 'string' ? search.token_name : undefined,
    model_name:
      typeof search.model_name === 'string' ? search.model_name : undefined,
    channel_id:
      typeof search.channel_id === 'string' ? toId(search.channel_id) : undefined,
    group: typeof search.group === 'string' ? search.group : undefined,
    matched_keyword:
      typeof search.matched_keyword === 'string'
        ? search.matched_keyword
        : undefined,
    request_id:
      typeof search.request_id === 'string' ? search.request_id : undefined,
    start_timestamp:
      typeof search.startTime === 'number'
        ? toApiTimestamp(search.startTime)
        : undefined,
    end_timestamp:
      typeof search.endTime === 'number'
        ? toApiTimestamp(search.endTime)
        : undefined,
  }
}

function useWarningLogsColumns(opts: {
  onViewDetails: (id: number) => void
}): ColumnDef<WarningLog>[] {
  const { t } = useTranslation()

  return useMemo(
    () => [
      {
        accessorKey: 'created_at',
        meta: { label: t('Time'), mobileTitle: true },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Time')} />
        ),
        cell: ({ row }) => (
          <div className='min-w-[150px] font-mono text-xs tabular-nums'>
            {formatTimestampToDate(row.original.created_at, 'seconds')}
          </div>
        ),
      },
      {
        accessorKey: 'username',
        meta: { label: t('User') },
        header: t('User'),
        cell: ({ row }) => (
          <span className='font-mono text-xs'>
            {row.original.username
              ? `${row.original.username} (#${row.original.user_id})`
              : `#${row.original.user_id}`}
          </span>
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
        accessorKey: 'model_name',
        meta: { label: t('Model') },
        header: t('Model'),
        cell: ({ row }) => (
          <Badge variant='secondary' className='font-mono'>
            {row.original.model_name || '-'}
          </Badge>
        ),
      },
      {
        accessorKey: 'group',
        meta: { label: t('Group') },
        header: t('Group'),
        cell: ({ row }) => row.original.group || '-',
      },
      {
        accessorKey: 'matched_keyword',
        meta: { label: t('Matched Keyword') },
        header: t('Matched Keyword'),
        cell: ({ row }) => (
          <span className='block max-w-[220px] truncate font-mono text-xs'>
            {row.original.matched_keyword || '-'}
          </span>
        ),
      },
      {
        accessorKey: 'status_code',
        meta: { label: t('Status') },
        header: t('Status'),
        cell: ({ row }) => (
          <div className='flex flex-col gap-0.5'>
            <StatusBadge
              label={String(row.original.status_code)}
              variant='red'
              size='sm'
              copyable={false}
            />
            {row.original.error_code && (
              <span className='text-muted-foreground/70 text-[11px]'>
                {row.original.error_code}
              </span>
            )}
          </div>
        ),
      },
      {
        accessorKey: 'request_id',
        meta: { label: t('Request ID') },
        header: t('Request ID'),
        cell: ({ row }) => (
          <span className='block max-w-[160px] truncate font-mono text-xs'>
            {row.original.request_id || '-'}
          </span>
        ),
      },
      {
        id: 'actions',
        meta: { label: t('Actions') },
        enableHiding: false,
        enableSorting: false,
        cell: ({ row }) => (
          <Button
            variant='outline'
            size='sm'
            className='h-7 gap-1.5 px-2 text-xs'
            onClick={() => opts.onViewDetails(row.original.id)}
          >
            {t('View Details')}
          </Button>
        ),
      },
    ],
    [opts, t]
  )
}

export function WarningLogsTable() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const searchParams = route.useSearch()
  const [detailsLogId, setDetailsLogId] = useState<number | null>(null)

  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search: searchParams,
      navigate: route.useNavigate(),
      pagination: {
        pageKey: 'p',
        pageSizeKey: 'page_size',
        defaultPage: 1,
        defaultPageSize: isMobile ? 20 : 50,
      },
      globalFilter: { enabled: false },
    })

  const columns = useWarningLogsColumns({
    onViewDetails: (id) => setDetailsLogId(id),
  })

  const { data, isLoading, isFetching } = useQuery({
    queryKey: ['warning-logs', searchParams],
    queryFn: async () => {
      const result = await getAllWarningLogs(
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
    <>
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No Warning Logs Found')}
        emptyDescription={t(
          'No content-policy warning logs available. Entries appear here when an upstream provider rejects a request for violating its content policy.'
        )}
        skeletonKeyPrefix='warning-log-skeleton'
        tableClassName={cn(
          'overflow-x-auto',
          '[&_[data-slot=table]]:text-[13px] [&_[data-slot=table]_td]:text-[13px] [&_[data-slot=table]_td_*]:text-[13px] [&_[data-slot=table]_th]:text-[13px] [&_[data-slot=table]_th_*]:text-[13px]'
        )}
        tableHeaderClassName='bg-muted/30 sticky top-0 z-10'
        toolbar={<WarningLogsFilterBar table={table} />}
      />
      <WarningLogDetailsDialog
        logId={detailsLogId}
        open={detailsLogId !== null}
        onOpenChange={(open) => {
          if (!open) setDetailsLogId(null)
        }}
      />
    </>
  )
}
