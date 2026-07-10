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
import { useQuery } from '@tanstack/react-query'
import {
  type SortingState,
  getCoreRowModel,
  getSortedRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { AlertCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { computeTimeRange } from '@/lib/time'
import { useIsAdmin } from '@/hooks/use-admin'
import { Button } from '@/components/ui/button'
import { DataTablePage } from '@/components/data-table'
import { getKeyDistribution } from '@/features/dashboard/api'
import { buildQueryParams, getDefaultDays } from '@/features/dashboard/lib'
import type { DashboardFilters } from '@/features/dashboard/types'
import { useKeyColumns } from './key-columns'

interface KeyTableProps {
  filters?: DashboardFilters
}

export function KeyTable(props: KeyTableProps) {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const columns = useKeyColumns()
  const [sorting, setSorting] = useState<SortingState>([
    { id: 'total_tokens', desc: true },
  ])

  const timeRange = computeTimeRange(
    getDefaultDays(props.filters?.time_granularity),
    props.filters?.start_timestamp,
    props.filters?.end_timestamp
  )
  // XX: the self endpoint always resolves the acting user from the auth
  // session, so only forward the username filter on admin requests.
  const username = isAdmin ? props.filters?.username : undefined

  const { data, isLoading, isFetching, isError, refetch } = useQuery({
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

  const table = useReactTable({
    data: data ?? [],
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      isFetching={isFetching}
      toolbarProps={null}
      showPagination={false}
      skeletonKeyPrefix='key-stats-skeleton'
      emptyIcon={isError ? <AlertCircle className='size-6' /> : undefined}
      emptyTitle={isError ? t('Loading failed') : t('No Key Usage Found')}
      emptyDescription={
        isError
          ? t('Failed to load')
          : t('No key usage data available for the selected time range.')
      }
      emptyAction={
        isError ? (
          <Button variant='outline' size='sm' onClick={() => refetch()}>
            {t('Retry')}
          </Button>
        ) : undefined
      }
    />
  )
}
