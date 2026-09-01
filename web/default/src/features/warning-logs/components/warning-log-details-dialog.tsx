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
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Check, Copy, Loader2, RefreshCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { StatusBadge } from '@/components/status-badge'
import { getWarningLog } from '../api'

interface WarningLogDetailsDialogProps {
  logId: number | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

function DetailRow(props: { label: string; value: React.ReactNode }) {
  return (
    <div className='grid min-w-0 grid-cols-[6.5rem_minmax(0,1fr)] gap-2 text-sm sm:grid-cols-[7.5rem_minmax(0,1fr)]'>
      <span className='text-muted-foreground min-w-0 text-xs'>
        {props.label}
      </span>
      <span className='max-w-full min-w-0 font-mono text-xs break-all'>
        {props.value}
      </span>
    </div>
  )
}

function CopyIconButton(props: { text: string; className?: string }) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({
    notify: false,
  })
  const copied = copiedText === props.text

  if (!props.text) return null

  return (
    <Button
      type='button'
      variant='ghost'
      size='sm'
      className={cn('h-6 w-6 p-0', props.className)}
      onClick={() => copyToClipboard(props.text)}
      title={t('Copy to clipboard')}
      aria-label={t('Copy to clipboard')}
    >
      {copied ? (
        <Check className='size-3 text-green-600' />
      ) : (
        <Copy className='size-3' />
      )}
    </Button>
  )
}

function formatBodySize(bytes: number): string {
  if (!bytes) return '0 B'
  if (bytes < 1024) return `${bytes} B`
  return `${(bytes / 1024).toFixed(1)} KB`
}

export function WarningLogDetailsDialog(props: WarningLogDetailsDialogProps) {
  const { t } = useTranslation()

  const {
    data: detailsRes,
    isLoading,
    isFetching,
    refetch,
  } = useQuery({
    queryKey: ['warning-log-details', props.logId],
    queryFn: () => (props.logId ? getWarningLog(props.logId) : null),
    enabled: props.open && props.logId !== null,
  })

  const log = detailsRes?.data

  const requestBodyPretty = useMemo(() => {
    if (!log?.request_body) return ''
    try {
      return JSON.stringify(JSON.parse(log.request_body), null, 2)
    } catch {
      return log.request_body
    }
  }, [log?.request_body])

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent className='min-w-0 overflow-hidden max-sm:max-h-[calc(100dvh-1.5rem)] max-sm:w-[calc(100vw-1.5rem)] max-sm:max-w-[calc(100vw-1.5rem)] sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2 text-base'>
            {t('Warning Log Details')}
            {log && (
              <StatusBadge
                label={String(log.status_code)}
                variant='red'
                size='sm'
                copyable={false}
              />
            )}
          </DialogTitle>
          <DialogDescription className='sr-only'>
            {t('View the complete details for this warning log entry')}
          </DialogDescription>
        </DialogHeader>

        <ScrollArea className='max-h-[70vh] min-w-0 overflow-hidden pr-2 max-sm:max-h-[calc(100dvh-9rem)] sm:pr-4'>
          <div className='w-full max-w-full min-w-0 space-y-3 overflow-hidden py-1'>
            <div className='flex items-center justify-end gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => refetch()}
                disabled={isFetching}
              >
                {isFetching ? (
                  <Loader2 className='size-3.5 animate-spin' />
                ) : (
                  <RefreshCcw className='size-3.5' />
                )}
                {t('Refresh')}
              </Button>
            </div>

            {isLoading ? (
              <div className='flex items-center justify-center py-10'>
                <Loader2 className='text-muted-foreground size-6 animate-spin' />
              </div>
            ) : !detailsRes?.success || !log ? (
              <div className='text-muted-foreground py-10 text-center text-sm'>
                {detailsRes?.message || t('Failed to fetch warning log details')}
              </div>
            ) : (
              <>
                <div className='bg-muted/30 min-w-0 space-y-1.5 overflow-hidden rounded-md border p-2.5'>
                  <DetailRow
                    label={t('Time')}
                    value={formatTimestampToDate(log.created_at, 'seconds')}
                  />
                  <DetailRow
                    label={t('User')}
                    value={`${log.username || '-'} (#${log.user_id})`}
                  />
                  <DetailRow
                    label={t('Token')}
                    value={log.token_name || '-'}
                  />
                  <DetailRow
                    label={t('Channel')}
                    value={`${log.channel_name || '-'} (#${log.channel_id})`}
                  />
                  <DetailRow label={t('Model')} value={log.model_name || '-'} />
                  <DetailRow label={t('Group')} value={log.group || '-'} />
                  <DetailRow label={t('IP Address')} value={log.ip || '-'} />
                  <DetailRow
                    label={t('Request Path')}
                    value={log.request_path || '-'}
                  />
                  <DetailRow
                    label={t('Matched Keyword')}
                    value={log.matched_keyword || '-'}
                  />
                  <DetailRow
                    label={t('Error Code')}
                    value={log.error_code || '-'}
                  />
                  {log.request_id && (
                    <DetailRow
                      label={t('Request ID')}
                      value={
                        <span className='inline-flex items-center gap-1'>
                          {log.request_id}
                          <CopyIconButton text={log.request_id} />
                        </span>
                      }
                    />
                  )}
                  {log.upstream_request_id && (
                    <DetailRow
                      label={t('Upstream Request ID')}
                      value={log.upstream_request_id}
                    />
                  )}
                  <DetailRow
                    label={t('Body Size')}
                    value={formatBodySize(log.body_size)}
                  />
                </div>

                <div className='space-y-1.5'>
                  <Label className='text-destructive text-xs font-semibold'>
                    {t('Error Message')}
                  </Label>
                  <div className='border-red-200 bg-red-50 dark:border-red-900 dark:bg-red-950/20 min-w-0 overflow-hidden rounded-md border p-2.5'>
                    <p className='min-w-0 text-xs leading-relaxed break-all whitespace-pre-wrap'>
                      {log.error_message || '-'}
                    </p>
                  </div>
                </div>

                {log.prompt_text && (
                  <div className='space-y-1.5'>
                    <div className='flex items-center justify-between'>
                      <Label className='text-xs font-semibold'>
                        {t('Prompt Text')}
                      </Label>
                      <CopyIconButton text={log.prompt_text} />
                    </div>
                    <div className='bg-muted/30 min-w-0 max-h-64 overflow-y-auto rounded-md border p-2.5'>
                      <p className='min-w-0 text-xs leading-relaxed break-all whitespace-pre-wrap'>
                        {log.prompt_text}
                      </p>
                    </div>
                  </div>
                )}

                <Collapsible className='rounded-lg border p-3'>
                  <div className='flex items-center justify-between gap-2'>
                    <CollapsibleTrigger className='cursor-pointer text-sm font-medium'>
                      {t('Request Body')}
                    </CollapsibleTrigger>
                    <CopyIconButton text={requestBodyPretty} />
                  </div>
                  <CollapsibleContent>
                    <pre className='mt-3 max-h-[360px] overflow-auto rounded-md bg-black p-3 text-xs text-gray-200'>
                      {requestBodyPretty || '-'}
                    </pre>
                  </CollapsibleContent>
                </Collapsible>
              </>
            )}
          </div>
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
