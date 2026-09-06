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
import { useCallback, useEffect, useState } from 'react'
import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import { getRouteApi, useNavigate } from '@tanstack/react-router'
import { type Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { CompactDateTimeRangePicker } from '@/features/usage-logs/components/compact-date-time-range-picker'
import {
  LogsFilterField,
  LogsFilterInput,
  LogsFilterToolbar,
} from '@/features/usage-logs/components/logs-filter-toolbar'
import type { WarningLogsFilters } from '../types'

const route = getRouteApi('/_authenticated/warning-logs/')

function getDate(value?: number): Date | undefined {
  return value ? new Date(value) : undefined
}

interface WarningLogsFilterBarProps<TData> {
  table: Table<TData>
}

export function WarningLogsFilterBar<TData>(
  props: WarningLogsFilterBarProps<TData>
) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const searchParams = route.useSearch()
  const fetchingLogs = useIsFetching({ queryKey: ['warning-logs'] })

  const [filters, setFilters] = useState<WarningLogsFilters>({})

  useEffect(() => {
    setFilters({
      startTime: getDate(searchParams.startTime),
      endTime: getDate(searchParams.endTime),
      userId: searchParams.user_id || undefined,
      username: searchParams.username || undefined,
      tokenName: searchParams.token_name || undefined,
      modelName: searchParams.model_name || undefined,
      channelId: searchParams.channel_id || undefined,
      group: searchParams.group || undefined,
      matchedKeyword: searchParams.matched_keyword || undefined,
      requestId: searchParams.request_id || undefined,
    })
  }, [
    searchParams.startTime,
    searchParams.endTime,
    searchParams.user_id,
    searchParams.username,
    searchParams.token_name,
    searchParams.model_name,
    searchParams.channel_id,
    searchParams.group,
    searchParams.matched_keyword,
    searchParams.request_id,
  ])

  const handleChange = useCallback(
    (field: keyof WarningLogsFilters, value: Date | string | undefined) => {
      setFilters((prev) => ({ ...prev, [field]: value }))
    },
    []
  )

  const handleApply = useCallback(() => {
    void navigate({
      to: '/warning-logs',
      search: {
        p: 1,
        page_size: searchParams.page_size,
        user_id: filters.userId || undefined,
        username: filters.username || undefined,
        token_name: filters.tokenName || undefined,
        model_name: filters.modelName || undefined,
        channel_id: filters.channelId || undefined,
        group: filters.group || undefined,
        matched_keyword: filters.matchedKeyword || undefined,
        request_id: filters.requestId || undefined,
        startTime: filters.startTime?.getTime(),
        endTime: filters.endTime?.getTime(),
      },
    })
    queryClient.invalidateQueries({ queryKey: ['warning-logs'] })
  }, [filters, navigate, queryClient, searchParams.page_size])

  const handleReset = useCallback(() => {
    setFilters({})
    void navigate({
      to: '/warning-logs',
      search: { p: 1, page_size: searchParams.page_size },
    })
    queryClient.invalidateQueries({ queryKey: ['warning-logs'] })
  }, [navigate, queryClient, searchParams.page_size])

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleApply()
    },
    [handleApply]
  )

  const hasActiveFilters = Boolean(
    filters.startTime ||
      filters.endTime ||
      filters.userId ||
      filters.username ||
      filters.tokenName ||
      filters.modelName ||
      filters.channelId ||
      filters.group ||
      filters.matchedKeyword ||
      filters.requestId
  )

  const dateRangeFilter = (
    <LogsFilterField wide>
      <CompactDateTimeRangePicker
        start={filters.startTime}
        end={filters.endTime}
        onChange={(range) => {
          handleChange('startTime', range.start)
          handleChange('endTime', range.end)
        }}
      />
    </LogsFilterField>
  )
  const modelFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Model Name')}
        value={filters.modelName || ''}
        onChange={(e) => handleChange('modelName', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const matchedKeywordFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Matched Keyword')}
        value={filters.matchedKeyword || ''}
        onChange={(e) => handleChange('matchedKeyword', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const groupFilter = (
    <LogsFilterField>
      <LogsFilterInput
        placeholder={t('Group')}
        value={filters.group || ''}
        onChange={(e) => handleChange('group', e.target.value)}
        onKeyDown={handleKeyDown}
      />
    </LogsFilterField>
  )
  const advancedFilters = (
    <>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('User ID')}
          value={filters.userId || ''}
          onChange={(e) => handleChange('userId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Username')}
          value={filters.username || ''}
          onChange={(e) => handleChange('username', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Token Name')}
          value={filters.tokenName || ''}
          onChange={(e) => handleChange('tokenName', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Channel ID')}
          value={filters.channelId || ''}
          onChange={(e) => handleChange('channelId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
      <LogsFilterField>
        <LogsFilterInput
          placeholder={t('Request ID')}
          value={filters.requestId || ''}
          onChange={(e) => handleChange('requestId', e.target.value)}
          onKeyDown={handleKeyDown}
        />
      </LogsFilterField>
    </>
  )

  const expandedFilterCount = [
    filters.userId,
    filters.username,
    filters.tokenName,
    filters.channelId,
    filters.requestId,
  ].filter(Boolean).length

  return (
    <LogsFilterToolbar
      table={props.table}
      primaryFilters={
        <>
          {dateRangeFilter}
          {modelFilter}
          {matchedKeywordFilter}
          {groupFilter}
        </>
      }
      advancedFilters={advancedFilters}
      mobilePinnedFilters={dateRangeFilter}
      mobileFilters={
        <>
          {modelFilter}
          {matchedKeywordFilter}
          {groupFilter}
          {advancedFilters}
        </>
      }
      mobileFilterCount={
        [filters.modelName, filters.matchedKeyword, filters.group].filter(
          Boolean
        ).length + expandedFilterCount
      }
      hasAdvancedActiveFilters={expandedFilterCount > 0}
      advancedFilterCount={expandedFilterCount}
      hasActiveFilters={hasActiveFilters}
      onSearch={handleApply}
      searchLoading={fetchingLogs > 0}
      onReset={handleReset}
    />
  )
}
