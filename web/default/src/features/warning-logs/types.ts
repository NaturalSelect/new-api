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
export interface WarningLog {
  id: number
  created_at: number
  user_id: number
  username: string
  token_id: number
  token_name: string
  channel_id: number
  channel_name: string
  channel_type: number
  model_name: string
  group: string
  ip: string
  request_id?: string
  upstream_request_id?: string
  status_code: number
  error_code: string
  matched_keyword: string
  error_message: string
  request_path: string
  prompt_text: string
  request_body: string
  body_size: number
  other: string
}

export interface GetWarningLogsParams {
  p?: number
  page_size?: number
  user_id?: number
  username?: string
  token_name?: string
  model_name?: string
  channel_id?: number
  group?: string
  matched_keyword?: string
  request_id?: string
  start_timestamp?: number
  end_timestamp?: number
}

export interface GetWarningLogsResponse {
  success: boolean
  message?: string
  data?: {
    items: WarningLog[]
    total: number
    page: number
    page_size: number
  }
}

export interface GetWarningLogResponse {
  success: boolean
  message?: string
  data?: WarningLog
}

export interface DeleteOldWarningLogsResponse {
  success: boolean
  message: string
  data?: number
}

export interface WarningLogsFilters {
  startTime?: Date
  endTime?: Date
  userId?: string
  username?: string
  tokenName?: string
  modelName?: string
  channelId?: string
  group?: string
  matchedKeyword?: string
  requestId?: string
}
