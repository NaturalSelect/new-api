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
import {
  CalendarClock,
  FileText,
  Layers,
  Maximize2,
  Sparkles,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { formatTokenCount, formatYearMonth } from '../lib/model-metadata'
import type { Modality, PricingModel } from '../types'
import { ModalityIcons } from './model-details-modalities'

type QuickStatsProps = {
  model: PricingModel
}

type Stat = {
  key: string
  icon: React.ComponentType<{ className?: string }>
  label: string
  value: React.ReactNode
  hint?: string
}

function buildStats(model: PricingModel, t: (key: string) => string): Stat[] {
  const stats: Stat[] = []
  const inputModalities = model.input_modalities ?? []
  const outputModalities = model.output_modalities ?? []

  if (model.context_length != null && model.context_length > 0) {
    stats.push({
      key: 'context',
      icon: Layers,
      label: t('Context'),
      value: formatTokenCount(model.context_length),
      hint: t('Maximum input window'),
    })
  }

  if (model.max_output_tokens != null && model.max_output_tokens > 0) {
    stats.push({
      key: 'max-output',
      icon: Maximize2,
      label: t('Max output'),
      value: formatTokenCount(model.max_output_tokens),
      hint: t('Maximum tokens per response'),
    })
  }

  if (inputModalities.length > 0 || outputModalities.length > 0) {
    stats.push({
      key: 'modalities',
      icon: FileText,
      label: t('Modalities'),
      value: <ModalityFlow input={inputModalities} output={outputModalities} />,
    })
  }

  if (model.knowledge_cutoff) {
    stats.push({
      key: 'knowledge',
      icon: Sparkles,
      label: t('Knowledge cutoff'),
      value: formatYearMonth(model.knowledge_cutoff),
    })
  }

  if (model.release_date) {
    stats.push({
      key: 'release',
      icon: CalendarClock,
      label: t('Released'),
      value: formatYearMonth(model.release_date),
    })
  }

  return stats
}

function ModalityFlow(props: { input: Modality[]; output: Modality[] }) {
  return (
    <span className='inline-flex items-center gap-1 align-middle'>
      <ModalityIcons modalities={props.input} className='size-3.5' />
      <span className='text-muted-foreground/40'>→</span>
      <ModalityIcons modalities={props.output} className='size-3.5' />
    </span>
  )
}

export function ModelDetailsQuickStats(props: QuickStatsProps) {
  const { t } = useTranslation()
  const stats = buildStats(props.model, t)

  if (stats.length === 0) return null

  return (
    <div className='bg-muted/20 grid grid-cols-2 gap-px overflow-hidden rounded-lg border @md/details:grid-cols-3 @2xl/details:grid-cols-5'>
      {stats.map((stat) => {
        const Icon = stat.icon
        return (
          <div
            key={stat.key}
            className={cn(
              'bg-background flex min-w-0 flex-col gap-0.5 px-3 py-2.5'
            )}
          >
            <span className='text-muted-foreground inline-flex min-w-0 items-center gap-1 text-[10px] font-medium tracking-wider uppercase'>
              <Icon className='size-3 shrink-0' />
              <span className='truncate'>{stat.label}</span>
            </span>
            <span className='text-foreground truncate text-sm font-semibold tabular-nums'>
              {stat.value}
            </span>
            {stat.hint && (
              <span className='text-muted-foreground/60 truncate text-[10px]'>
                {stat.hint}
              </span>
            )}
          </div>
        )
      })}
    </div>
  )
}
