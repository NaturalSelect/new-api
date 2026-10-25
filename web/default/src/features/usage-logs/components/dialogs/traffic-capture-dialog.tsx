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
import { useMemo, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { Check, ChevronRight, Copy, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatBytes, formatTimestampToDate } from '@/lib/format'
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
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import { getTrafficCaptureRecord } from '../../api'
import type {
  CapturedBody,
  CapturedExchange,
  CapturedUpstreamAttempt,
} from '../../types'

const NOT_FOUND_MESSAGE = 'not captured or expired'

type BodyKind = 'json' | 'text' | 'binary' | 'empty'

function formatBody(body: CapturedBody): { text: string; kind: BodyKind } {
  if (body.data === '') return { text: '', kind: 'empty' }
  if (body.encoding === 'base64') return { text: body.data, kind: 'binary' }

  // NOTE: JSON.parse rounds integers beyond 2^53, which would show a wrong ID
  // in a tool meant for exact traffic inspection, so such bodies stay raw.
  if (/^\s*[{[]/.test(body.data) && !/\d{16,}/.test(body.data)) {
    try {
      return {
        text: JSON.stringify(JSON.parse(body.data), null, 2),
        kind: 'json',
      }
    } catch {
      // Truncated or non-JSON payloads fall through to the raw text.
    }
  }
  return { text: body.data, kind: 'text' }
}

function formatMs(ms: number, t: TFunction): string {
  return ms < 1000
    ? t('{{value}}ms', { value: ms })
    : t('{{value}}s', { value: (ms / 1000).toFixed(2) })
}

function getStatusVariant(status: number): StatusBadgeProps['variant'] {
  if (status >= 500) return 'danger'
  if (status >= 400) return 'warning'
  if (status >= 200 && status < 300) return 'success'
  return 'neutral'
}

function InfoRow(props: { label: string; value: ReactNode; mono?: boolean }) {
  return (
    <div className='grid min-w-0 grid-cols-[6rem_minmax(0,1fr)] gap-2 text-xs'>
      <span className='text-muted-foreground'>{props.label}</span>
      <span className={cn('min-w-0 break-all', props.mono && 'font-mono')}>
        {props.value}
      </span>
    </div>
  )
}

function Section(props: {
  title: string
  badge?: ReactNode
  children: ReactNode
}) {
  return (
    <section className='min-w-0 space-y-2'>
      <h4 className='flex items-center gap-2 text-sm font-semibold'>
        {props.title}
        {props.badge}
      </h4>
      <div className='bg-muted/30 min-w-0 space-y-3 rounded-md border p-2.5'>
        {props.children}
      </div>
    </section>
  )
}

const preClassName =
  'bg-background max-h-72 overflow-auto rounded border p-2 font-mono text-[11px] leading-relaxed break-all whitespace-pre-wrap'

function HeadersBlock(props: {
  label: string
  headers: Record<string, string[]> | null
}) {
  const lines = Object.entries(props.headers ?? {}).flatMap(([name, values]) =>
    values.map((value) => `${name}: ${value}`)
  )

  return (
    <div className='min-w-0 space-y-1.5'>
      <Label className='text-xs font-semibold'>{props.label}</Label>
      {lines.length > 0 ? (
        <pre className={preClassName}>{lines.join('\n')}</pre>
      ) : (
        <p className='text-muted-foreground text-xs'>-</p>
      )}
    </div>
  )
}

function BodyBlock(props: { label: string; body: CapturedBody }) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  const { body } = props
  const formatted = useMemo(() => formatBody(body), [body])
  const hasTiming = body.first_byte_ms > 0 || body.last_byte_ms > 0

  return (
    <div className='min-w-0 space-y-1.5'>
      <div className='flex flex-wrap items-center gap-x-2 gap-y-1'>
        <Label className='text-xs font-semibold'>{props.label}</Label>
        {formatted.kind === 'json' && (
          <StatusBadge label='JSON' variant='info' copyable={false} />
        )}
        {formatted.kind === 'binary' && (
          <StatusBadge
            label={t('Binary (base64)')}
            variant='purple'
            copyable={false}
          />
        )}
        {body.truncated && (
          <StatusBadge
            label={t('Truncated')}
            variant='warning'
            copyable={false}
          />
        )}
        {formatted.kind !== 'empty' && (
          <Button
            type='button'
            variant='ghost'
            size='icon-xs'
            className='ml-auto'
            onClick={() => copyToClipboard(formatted.text)}
            title={t('Copy to clipboard')}
            aria-label={t('Copy to clipboard')}
          >
            {copiedText === formatted.text ? (
              <Check className='text-green-600' />
            ) : (
              <Copy />
            )}
          </Button>
        )}
      </div>

      <div className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-0.5 text-[11px]'>
        <span>
          {t('Original size')}: {formatBytes(body.size)}
        </span>
        {hasTiming && (
          <>
            <span>
              {t('First byte')}: {formatMs(body.first_byte_ms, t)}
            </span>
            <span>
              {t('Last byte')}: {formatMs(body.last_byte_ms, t)}
            </span>
          </>
        )}
      </div>

      {formatted.kind === 'binary' && (
        <p className='text-muted-foreground text-[11px]'>
          {t('Binary content, shown as base64 text.')}
        </p>
      )}

      {formatted.kind === 'empty' ? (
        <p className='text-muted-foreground text-xs'>{t('Empty')}</p>
      ) : (
        <pre className={preClassName}>{formatted.text}</pre>
      )}
    </div>
  )
}

function RequestPart(props: { exchange: CapturedExchange }) {
  const { t } = useTranslation()
  const { exchange } = props

  return (
    <>
      <div className='flex min-w-0 items-start gap-2 text-xs'>
        <StatusBadge
          label={exchange.method}
          variant='info'
          copyable={false}
          className='mt-px'
        />
        <span className='min-w-0 font-mono break-all'>{exchange.url}</span>
      </div>
      <HeadersBlock
        label={t('Request Headers')}
        headers={exchange.request_headers}
      />
      <BodyBlock label={t('Request Body')} body={exchange.request_body} />
    </>
  )
}

function ResponsePart(props: { exchange: CapturedExchange }) {
  const { t } = useTranslation()
  const { exchange } = props

  return (
    <>
      <InfoRow
        label={t('Status')}
        value={
          exchange.status > 0 ? (
            <StatusBadge
              label={String(exchange.status)}
              variant={getStatusVariant(exchange.status)}
              copyable={false}
            />
          ) : (
            '-'
          )
        }
      />
      <HeadersBlock
        label={t('Response Headers')}
        headers={exchange.response_headers}
      />
      <BodyBlock label={t('Response Body')} body={exchange.response_body} />
    </>
  )
}

function UpstreamAttemptItem(props: {
  attempt: CapturedUpstreamAttempt
  position: number
  defaultOpen: boolean
}) {
  const { t } = useTranslation()
  const { attempt } = props

  return (
    <Collapsible
      defaultOpen={props.defaultOpen}
      className='bg-background min-w-0 rounded-md border'
    >
      <CollapsibleTrigger
        render={
          <Button
            type='button'
            variant='ghost'
            className='group h-auto w-full justify-start gap-2 p-2.5 text-xs whitespace-normal'
          />
        }
      >
        <ChevronRight className='transition-transform group-data-[panel-open]:rotate-90' />
        <span className='font-medium'>
          {t('Attempt {{position}}', { position: props.position })}
        </span>
        {attempt.status > 0 && (
          <StatusBadge
            label={String(attempt.status)}
            variant={getStatusVariant(attempt.status)}
            copyable={false}
          />
        )}
        {attempt.error && (
          <StatusBadge label={t('Error')} variant='danger' copyable={false} />
        )}
        <span className='text-muted-foreground font-mono font-normal'>
          {t('Channel #{{id}}', { id: attempt.channel_id })}
        </span>
        <span className='text-muted-foreground font-mono font-normal'>
          {t('Retry #{{index}}', { index: attempt.retry_index })}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent className='min-w-0 space-y-3 border-t p-2.5'>
        {attempt.error && (
          <InfoRow
            label={t('Error Message')}
            value={
              <span className='text-destructive whitespace-pre-wrap'>
                {attempt.error}
              </span>
            }
          />
        )}
        <RequestPart exchange={attempt} />
        <ResponsePart exchange={attempt} />
      </CollapsibleContent>
    </Collapsible>
  )
}

interface TrafficCaptureDialogProps {
  requestId: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function TrafficCaptureDialog(props: TrafficCaptureDialogProps) {
  const { t } = useTranslation()

  const query = useQuery({
    queryKey: ['traffic-capture-record', props.requestId],
    queryFn: () => getTrafficCaptureRecord(props.requestId),
    enabled: props.open,
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: 30_000,
  })

  const response = query.data
  const capture = response?.success ? response.data : undefined
  const record = capture?.record
  const upstream = record?.upstream ?? []
  const notFoundDetail =
    response && !response.success && response.message !== NOT_FOUND_MESSAGE
      ? response.message
      : null

  return (
    <Dialog open={props.open} onOpenChange={props.onOpenChange}>
      <DialogContent
        className={cn(
          'min-w-0 overflow-hidden',
          'max-sm:max-h-[calc(100dvh-1.5rem)] max-sm:w-[calc(100vw-1.5rem)] max-sm:max-w-[calc(100vw-1.5rem)] max-sm:p-4',
          'sm:max-w-3xl'
        )}
      >
        <DialogHeader className='max-sm:gap-1'>
          <DialogTitle>{t('Traffic Capture')}</DialogTitle>
          <DialogDescription>
            {t(
              'Full request and response as seen by the gateway, including every upstream attempt. Bodies are not redacted.'
            )}
          </DialogDescription>
        </DialogHeader>

        {query.isLoading ? (
          <div className='flex items-center justify-center py-12'>
            <Loader2 className='text-muted-foreground size-6 animate-spin' />
          </div>
        ) : query.isError ? (
          <div className='flex flex-col items-center gap-3 py-12 text-sm'>
            <p className='text-muted-foreground'>{t('Failed to load')}</p>
            <Button variant='outline' size='sm' onClick={() => query.refetch()}>
              {t('Retry')}
            </Button>
          </div>
        ) : !capture || !record ? (
          <div className='text-muted-foreground space-y-1 py-12 text-center text-sm'>
            <p>{t('Not captured or expired')}</p>
            {notFoundDetail && <p className='text-xs'>{notFoundDetail}</p>}
          </div>
        ) : (
          <ScrollArea className='max-h-[70vh] min-w-0 overflow-hidden pr-2 max-sm:max-h-[calc(100dvh-9rem)] sm:pr-4'>
            <div className='min-w-0 space-y-4 py-1'>
              <div className='bg-muted/30 grid min-w-0 gap-x-6 gap-y-1.5 rounded-md border p-2.5 sm:grid-cols-2'>
                <InfoRow
                  label={t('Request ID')}
                  value={record.request_id}
                  mono
                />
                <InfoRow
                  label={t('Result')}
                  value={
                    <StatusBadge
                      label={record.failed ? t('Failed') : t('Success')}
                      variant={record.failed ? 'danger' : 'success'}
                      copyable={false}
                    />
                  }
                />
                <InfoRow label={t('Model')} value={record.model_name || '-'} />
                <InfoRow label={t('Group')} value={record.group || '-'} />
                <InfoRow label={t('User ID')} value={record.user_id} mono />
                <InfoRow label={t('Token ID')} value={record.token_id} mono />
                <InfoRow
                  label={t('Client IP')}
                  value={record.client_ip || '-'}
                  mono
                />
                <InfoRow
                  label={t('Node Name')}
                  value={record.node_name || '-'}
                />
                <InfoRow
                  label={t('Start Time')}
                  value={formatTimestampToDate(
                    record.started_at_ms,
                    'milliseconds'
                  )}
                />
                <InfoRow
                  label={t('Duration')}
                  value={formatMs(record.duration_ms, t)}
                />
                <InfoRow
                  label={t('Captured at')}
                  value={formatTimestampToDate(capture.created_at)}
                />
                <InfoRow
                  label={t('Stored / raw size')}
                  value={`${formatBytes(capture.stored_size)} / ${formatBytes(capture.raw_size)}`}
                />
              </div>

              <Section title={t('Client Request')}>
                <RequestPart exchange={record.client} />
              </Section>

              <Section title={t('Client Response')}>
                <ResponsePart exchange={record.client} />
              </Section>

              <Section
                title={t('Upstream Attempts')}
                badge={
                  <StatusBadge
                    label={String(upstream.length)}
                    variant='neutral'
                    copyable={false}
                  />
                }
              >
                {upstream.length === 0 ? (
                  <p className='text-muted-foreground text-xs'>
                    {t('No upstream attempts were captured.')}
                  </p>
                ) : (
                  <div className='space-y-2'>
                    {upstream.map((attempt, index) => (
                      <UpstreamAttemptItem
                        key={`${attempt.retry_index}-${index}`}
                        attempt={attempt}
                        position={index + 1}
                        defaultOpen={upstream.length === 1}
                      />
                    ))}
                  </div>
                )}
              </Section>
            </div>
          </ScrollArea>
        )}
      </DialogContent>
    </Dialog>
  )
}
