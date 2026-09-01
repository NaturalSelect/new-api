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
import z from 'zod'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { WarningLogs } from '@/features/warning-logs'

const warningLogsSearchSchema = z.object({
  p: z.number().optional().catch(1),
  page_size: z.number().optional().catch(undefined),
  user_id: z.string().optional().catch(''),
  username: z.string().optional().catch(''),
  token_name: z.string().optional().catch(''),
  model_name: z.string().optional().catch(''),
  channel_id: z.string().optional().catch(''),
  group: z.string().optional().catch(''),
  matched_keyword: z.string().optional().catch(''),
  request_id: z.string().optional().catch(''),
  startTime: z.number().optional(),
  endTime: z.number().optional(),
})

export const Route = createFileRoute('/_authenticated/warning-logs/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({
        to: '/403',
      })
    }
  },
  validateSearch: warningLogsSearchSchema,
  component: WarningLogs,
})
