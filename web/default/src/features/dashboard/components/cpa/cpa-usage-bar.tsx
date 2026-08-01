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
import { useTranslation } from 'react-i18next'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'

function getUsageColorClass(percent: number): string {
  if (percent > 80) return 'bg-destructive'
  if (percent > 50) return 'bg-warning'
  return 'bg-success'
}

interface CPAUsageBarProps {
  label: string
  percent: number
  resetAt: string
}

export function CPAUsageBar(props: CPAUsageBarProps) {
  const { t } = useTranslation()
  const clamped = Math.min(100, Math.max(0, Math.round(props.percent)))
  const resetDisplay = dayjs(props.resetAt).isValid()
    ? dayjs(props.resetAt).fromNow()
    : null

  return (
    <div className='flex min-w-0 flex-col gap-1'>
      <div className='flex items-center justify-between gap-2 text-xs'>
        <span className='text-muted-foreground'>{props.label}</span>
        <span className='font-mono font-medium tabular-nums'>
          {clamped}%
        </span>
      </div>
      <div className='bg-muted h-1.5 w-full overflow-hidden rounded-full'>
        <div
          className={cn(
            'h-full rounded-full transition-all',
            getUsageColorClass(clamped)
          )}
          style={{ width: `${clamped}%` }}
        />
      </div>
      {resetDisplay && (
        <span className='text-muted-foreground/70 text-xs'>
          {t('resets')} {resetDisplay}
        </span>
      )}
    </div>
  )
}
