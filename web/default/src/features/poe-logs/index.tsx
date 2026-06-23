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
import { useIsAdmin } from '@/hooks/use-admin'
import { SectionPageLayout } from '@/components/layout'
import { PoeLogsTable } from './components/poe-logs-table'

export function PoeLogs() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()

  if (!isAdmin) return null

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Poe Logs')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <PoeLogsTable />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
