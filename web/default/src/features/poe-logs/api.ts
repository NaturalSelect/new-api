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
import { api } from '@/lib/api'
import type {
  GetPoeLogStatsParams,
  GetPoeLogStatsResponse,
  GetPoeLogsParams,
  GetPoeLogsResponse,
} from './types'

function buildQueryParams(params: Record<string, unknown>): URLSearchParams {
  const queryParams = new URLSearchParams()

  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      queryParams.append(key, String(value))
    }
  })

  return queryParams
}

export async function getAllPoeLogs(
  params: GetPoeLogsParams = {}
): Promise<GetPoeLogsResponse> {
  const queryParams = buildQueryParams({
    p: params.p || 1,
    page_size: params.page_size || 20,
    ...params,
  })
  const res = await api.get(`/api/poe_log/?${queryParams}`)
  return res.data
}

export async function getPoeLogStats(
  params: GetPoeLogStatsParams = {}
): Promise<GetPoeLogStatsResponse> {
  const queryParams = buildQueryParams(params as Record<string, unknown>)
  const res = await api.get(`/api/poe_log/stat?${queryParams}`)
  return res.data
}

export async function triggerPoeLogSync(
  channelId: number
): Promise<{ success: boolean; message: string }> {
  const res = await api.post('/api/poe_log/sync', { channel_id: channelId })
  return res.data
}
