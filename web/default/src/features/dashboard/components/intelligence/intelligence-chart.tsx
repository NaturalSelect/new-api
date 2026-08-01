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
import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import { BrainCircuit } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { VCHART_OPTION } from '@/lib/vchart'
import { useThemeCustomization } from '@/context/theme-customization-provider'
import { useTheme } from '@/context/theme-provider'
import { Skeleton } from '@/components/ui/skeleton'
import { getIntelligenceScores } from '@/features/dashboard/api'
import { processIntelligenceChartData } from '@/features/dashboard/lib'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

export function IntelligenceChart() {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const { customization } = useThemeCustomization()
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

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

  const { data, isLoading } = useQuery({
    queryKey: ['dashboard', 'intelligence-scores'],
    queryFn: getIntelligenceScores,
    select: (res) => (res.success ? res.data : { scores: [], updated_at: 0 }),
    staleTime: 60_000,
  })

  const scores = useMemo(() => data?.scores ?? [], [data])
  const updatedAt = data?.updated_at ?? 0
  const updatedAtDisplay = updatedAt
    ? new Date(updatedAt * 1000).toLocaleString()
    : null

  const chartData = useMemo(
    () =>
      processIntelligenceChartData(
        isLoading ? [] : scores,
        t,
        customization.preset
      ),
    [scores, isLoading, t, customization.preset]
  )

  const spec = chartData.spec_intelligence_line

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='flex w-full items-center justify-between gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
        <div className='flex items-center gap-2'>
          <BrainCircuit className='text-muted-foreground/60 size-4' />
          <div className='text-sm font-semibold'>
            {t('Model Intelligence Ranking')}
          </div>
        </div>
        {updatedAtDisplay && (
          <span className='text-muted-foreground text-xs'>
            {t('Last updated:')} {updatedAtDisplay}
          </span>
        )}
      </div>

      <div className='h-[420px] p-1.5 sm:p-2'>
        {isLoading ? (
          <Skeleton className='h-full w-full' />
        ) : (
          themeReady &&
          spec && (
            <VChart
              key={`intelligence-${scores.length}-${resolvedTheme}-${customization.preset}`}
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
