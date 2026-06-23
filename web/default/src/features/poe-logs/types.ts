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
export interface PoeLog {
  id: number
  channel_id: number
  channel_name?: string
  query_id: string
  bot_name: string
  creation_time: number
  cost_usd: string
  cost_points: number
  cost_breakdown: string
  usage_type: string
  api_key_name?: string
  chat_name?: string
  canvas_tab_name?: string
  prompt_tokens: number
  completion_tokens: number
  cache_tokens: number
  cache_write_tokens: number
  synced_at: number
}

export interface GetPoeLogsParams {
  p?: number
  page_size?: number
  channel_id?: number
  bot_name?: string
  usage_type?: string
  paid_only?: boolean
  start_timestamp?: number
  end_timestamp?: number
}

export interface GetPoeLogsResponse {
  success: boolean
  message?: string
  data?: {
    items: PoeLog[]
    total: number
    page: number
    page_size: number
  }
}

export interface PoeLogStats {
  total_points: number
  total_usd: string
  count: number
  total_prompt_tokens: number
  total_completion_tokens: number
  total_cache_tokens: number
  total_cache_write_tokens: number
  total_tokens: number
  total_cost_usd: string
}

export interface GetPoeLogStatsParams {
  channel_id?: number
  start_timestamp?: number
  end_timestamp?: number
  paid_only?: boolean
}

export interface GetPoeLogStatsResponse {
  success: boolean
  message?: string
  data?: PoeLogStats
}

export interface PoeLogsFilters {
  startTime?: Date
  endTime?: Date
  channelId?: string
  botName?: string
  usageType?: string
  paidOnly?: boolean
}
