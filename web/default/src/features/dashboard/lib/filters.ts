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
  getCurrentMonthDateRange,
  getRollingDateRange,
  type TimeGranularity,
} from '@/lib/time'
import {
  DASHBOARD_CHART_PREFERENCES_STORAGE_KEY,
  DASHBOARD_CHART_PREFERENCES_VERSION,
  DEFAULT_DASHBOARD_CHART_PREFERENCES,
  EMPTY_DASHBOARD_FILTERS,
  TIME_GRANULARITY_STORAGE_KEY,
  TIME_RANGE_PRESETS,
  TIME_RANGE_BY_GRANULARITY,
} from '@/features/dashboard/constants'
import type {
  ConsumptionDistributionChartType,
  ConsumptionDistributionMode,
  DashboardChartPreferences,
  DashboardFilters,
  ModelAnalyticsChartTab,
} from '@/features/dashboard/types'

function isTimeGranularity(value: unknown): value is TimeGranularity {
  return value === 'hour' || value === 'day' || value === 'week'
}

function isConsumptionDistributionChartType(
  value: unknown
): value is ConsumptionDistributionChartType {
  return value === 'bar' || value === 'area'
}

function isConsumptionDistributionMode(
  value: unknown
): value is ConsumptionDistributionMode {
  return value === 'quota' || value === 'token'
}

function isTimeRangePresetDays(value: unknown): value is number {
  return TIME_RANGE_PRESETS.some((preset) => preset.days === value)
}

export function cleanFilters<T extends Record<string, unknown>>(
  filters: T
): Partial<T> {
  const cleaned: Partial<T> = {}
  for (const [key, value] of Object.entries(filters)) {
    if (value === undefined || value === null) continue
    if (typeof value === 'string') {
      const trimmed = value.trim()
      if (trimmed) cleaned[key as keyof T] = trimmed as T[keyof T]
      continue
    }
    cleaned[key as keyof T] = value as T[keyof T]
  }
  return cleaned
}

export function getDashboardDateRange(days: number): { start: Date; end: Date } {
  return days === 0 ? getCurrentMonthDateRange() : getRollingDateRange(days)
}

export function getSavedGranularity(
  override?: TimeGranularity
): TimeGranularity {
  if (override) return override
  return getSavedChartPreferences().defaultTimeGranularity
}

export function saveGranularity(granularity: TimeGranularity): void {
  if (typeof window === 'undefined') return
  saveChartPreferences({
    ...getSavedChartPreferences(),
    defaultTimeGranularity: granularity,
  })
  localStorage.setItem(TIME_GRANULARITY_STORAGE_KEY, granularity)
}

export function getSavedChartPreferences(): DashboardChartPreferences {
  if (typeof window === 'undefined') return DEFAULT_DASHBOARD_CHART_PREFERENCES

  const fallbackPreferences = DEFAULT_DASHBOARD_CHART_PREFERENCES

  try {
    const raw = localStorage.getItem(DASHBOARD_CHART_PREFERENCES_STORAGE_KEY)
    if (!raw) {
      const preferences = {
        ...fallbackPreferences,
        defaultTimeGranularity: fallbackPreferences.defaultTimeGranularity,
      }
      saveChartPreferences(preferences)
      return preferences
    }

    const parsed = JSON.parse(raw) as Partial<DashboardChartPreferences>
    const isCurrentVersion = parsed.version === DASHBOARD_CHART_PREFERENCES_VERSION
    const preferences: DashboardChartPreferences = {
      version: DASHBOARD_CHART_PREFERENCES_VERSION,
      consumptionDistributionChart: isConsumptionDistributionChartType(
        parsed.consumptionDistributionChart
      )
        ? parsed.consumptionDistributionChart
        : fallbackPreferences.consumptionDistributionChart,
      consumptionDistributionMode: isConsumptionDistributionMode(
        parsed.consumptionDistributionMode
      )
        ? parsed.consumptionDistributionMode
        : fallbackPreferences.consumptionDistributionMode,
      modelAnalyticsChart: isModelAnalyticsChartTab(parsed.modelAnalyticsChart)
        ? parsed.modelAnalyticsChart
        : fallbackPreferences.modelAnalyticsChart,
      defaultTimeRangeDays:
        isCurrentVersion && isTimeRangePresetDays(parsed.defaultTimeRangeDays)
          ? parsed.defaultTimeRangeDays
          : fallbackPreferences.defaultTimeRangeDays,
      defaultTimeGranularity:
        isCurrentVersion && isTimeGranularity(parsed.defaultTimeGranularity)
          ? parsed.defaultTimeGranularity
          : fallbackPreferences.defaultTimeGranularity,
    }

    if (!isCurrentVersion) saveChartPreferences(preferences)
    return preferences
  } catch {
    saveChartPreferences(fallbackPreferences)
    return fallbackPreferences
  }
}

export function saveChartPreferences(
  preferences: DashboardChartPreferences
): void {
  if (typeof window === 'undefined') return
  const nextPreferences = {
    ...preferences,
    version: DASHBOARD_CHART_PREFERENCES_VERSION,
  }
  localStorage.setItem(
    DASHBOARD_CHART_PREFERENCES_STORAGE_KEY,
    JSON.stringify(nextPreferences)
  )
  localStorage.setItem(
    TIME_GRANULARITY_STORAGE_KEY,
    nextPreferences.defaultTimeGranularity
  )
}

export function getDefaultDays(granularity?: TimeGranularity): number {
  const preferences = getSavedChartPreferences()
  if (!granularity || granularity === preferences.defaultTimeGranularity) {
    return preferences.defaultTimeRangeDays
  }
  return TIME_RANGE_BY_GRANULARITY[granularity]
}

export function buildDefaultDashboardFilters(
  preferences: DashboardChartPreferences = getSavedChartPreferences()
): DashboardFilters {
  const { start, end } = getDashboardDateRange(preferences.defaultTimeRangeDays)
  return {
    ...EMPTY_DASHBOARD_FILTERS,
    start_timestamp: start,
    end_timestamp: end,
    time_granularity: preferences.defaultTimeGranularity,
  }
}

export function buildQueryParams(
  timeRange: { start_timestamp: number; end_timestamp: number },
  filters?: { time_granularity?: TimeGranularity; username?: string }
): {
  start_timestamp: number
  end_timestamp: number
  default_time: string
  username?: string
} {
  return {
    ...timeRange,
    default_time: getSavedGranularity(filters?.time_granularity),
    ...(filters?.username && { username: filters.username }),
  }
}
