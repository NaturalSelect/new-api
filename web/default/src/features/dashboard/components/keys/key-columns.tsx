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
import { type ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { formatNumber } from '@/lib/format'
import { DataTableColumnHeader } from '@/components/data-table'
import type { KeyDistributionDataItem } from '@/features/dashboard/types'

export function useKeyColumns(): ColumnDef<KeyDistributionDataItem>[] {
  const { t } = useTranslation()

  return useMemo(
    (): ColumnDef<KeyDistributionDataItem>[] => [
      {
        accessorKey: 'token_id',
        meta: { label: t('Key ID') },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Key ID')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {row.original.token_id}
          </span>
        ),
      },
      {
        accessorKey: 'token_name',
        meta: { label: t('Key Name'), mobileTitle: true },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Key Name')} />
        ),
        cell: ({ row }) => row.original.token_name || '-',
      },
      {
        accessorKey: 'model_name',
        meta: { label: t('Model') },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Model')} />
        ),
        cell: ({ row }) => row.original.model_name || '-',
      },
      {
        accessorKey: 'input_tokens',
        meta: { label: t('Input Tokens') },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Input Tokens')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {formatNumber(row.original.input_tokens)}
          </span>
        ),
      },
      {
        accessorKey: 'output_tokens',
        meta: { label: t('Output Tokens') },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Output Tokens')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {formatNumber(row.original.output_tokens)}
          </span>
        ),
      },
      {
        accessorKey: 'total_tokens',
        meta: { label: t('Total Tokens') },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Total Tokens')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono font-semibold tabular-nums'>
            {formatNumber(row.original.total_tokens)}
          </span>
        ),
      },
      {
        accessorKey: 'count',
        meta: { label: t('Call Count') },
        header: ({ column }) => (
          <DataTableColumnHeader column={column} title={t('Call Count')} />
        ),
        cell: ({ row }) => (
          <span className='font-mono tabular-nums'>
            {formatNumber(row.original.count)}
          </span>
        ),
      },
    ],
    [t]
  )
}
