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
import { useEffect, useState } from 'react'
import { AlertTriangle, Check, Copy, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getRequestFailureLog } from '../../api'
import type { UsageLog } from '../../data/schema'
import type { RequestFailureLogDetail } from '../../types'

interface FailureRecordDialogProps {
  log: UsageLog | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

// Pretty-print JSON request bodies; fall back to the raw (unmasked) string.
function formatBody(value: string | undefined): string {
  if (!value) return ''
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

function MetaRow(props: { label: string; value: string }) {
  if (!props.value) return null
  return (
    <div className='min-w-0 space-y-1'>
      <span className='text-muted-foreground block text-[11px]'>
        {props.label}
      </span>
      <span className='block min-w-0 truncate font-mono text-xs'>
        {props.value}
      </span>
    </div>
  )
}

function PayloadPanel({
  title,
  value,
  emptyText,
}: {
  title: string
  value: string
  emptyText: string
}) {
  const { t } = useTranslation()
  const { copiedText, copyToClipboard } = useCopyToClipboard({ notify: false })
  return (
    <div className='min-w-0 space-y-2'>
      <div className='flex items-center justify-between gap-2'>
        <span className='text-muted-foreground text-xs font-medium'>
          {title}
        </span>
        <Button
          type='button'
          variant='ghost'
          size='sm'
          className='h-7 px-2'
          disabled={!value}
          onClick={() => copyToClipboard(value)}
          title={t('Copy to clipboard')}
          aria-label={t('Copy to clipboard')}
        >
          {copiedText === value ? (
            <Check className='size-3.5 text-green-600' />
          ) : (
            <Copy className='size-3.5' />
          )}
        </Button>
      </div>
      <ScrollArea className='bg-muted/30 h-[48vh] min-h-[18rem] overflow-hidden rounded-md border'>
        <pre
          className={cn(
            'min-w-0 p-3 font-mono text-xs leading-relaxed break-words whitespace-pre-wrap',
            !value && 'text-muted-foreground'
          )}
        >
          {value || emptyText}
        </pre>
      </ScrollArea>
    </div>
  )
}

export function FailureRecordDialog({
  log,
  open,
  onOpenChange,
}: FailureRecordDialogProps) {
  const { t } = useTranslation()
  const [detail, setDetail] = useState<RequestFailureLogDetail | null>(null)
  const [error, setError] = useState('')
  const [isLoading, setIsLoading] = useState(false)
  const [notFound, setNotFound] = useState(false)

  useEffect(() => {
    if (!open || !log?.id) return
    let cancelled = false
    setIsLoading(true)
    setError('')
    setNotFound(false)
    setDetail(null)
    getRequestFailureLog(log.id)
      .then((result) => {
        if (cancelled) return
        if (result.success) {
          if (result.data) setDetail(result.data)
          else setNotFound(true)
        } else {
          setError(result.message || t('Failed to load failure record'))
        }
      })
      .catch((err) => {
        if (cancelled) return
        // eslint-disable-next-line no-console
        console.error('Failed to load failure record:', err)
        setError(t('Failed to load failure record'))
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [log?.id, open, t])

  const requestBody = formatBody(detail?.request_body)
  const errorDetail = detail?.error_detail || ''
  const channelValue = detail?.channel_name
    ? `${detail.channel_name} #${detail.channel_id}`
    : detail?.channel_id
      ? `#${detail.channel_id}`
      : ''

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='min-w-0 overflow-hidden sm:max-w-4xl'>
        <DialogHeader>
          <DialogTitle>{t('Failure Record')}</DialogTitle>
          <DialogDescription>
            {t(
              'Raw captured request body and error of this failed request, for security troubleshooting.'
            )}
          </DialogDescription>
        </DialogHeader>

        {isLoading ? (
          <div className='flex items-center justify-center py-16'>
            <Loader2 className='text-muted-foreground size-6 animate-spin' />
          </div>
        ) : error ? (
          <Alert variant='destructive'>
            <AlertTriangle className='size-4' />
            <AlertTitle>{t('Failed to load failure record')}</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        ) : notFound || !detail ? (
          <div className='text-muted-foreground py-12 text-center text-sm'>
            {t(
              'No failure record. Enable "Record failed request body" in Log Maintenance to capture future failures.'
            )}
          </div>
        ) : (
          <div className='min-w-0 space-y-3'>
            <div className='grid min-w-0 grid-cols-1 gap-2 rounded-md border bg-muted/20 p-3 sm:grid-cols-2 lg:grid-cols-4'>
              <MetaRow
                label={t('Request ID')}
                value={detail.request_id || log?.request_id || ''}
              />
              <MetaRow
                label={t('Request Time')}
                value={formatTimestampToDate(detail.created_at)}
              />
              <MetaRow
                label={t('Status Code')}
                value={detail.status_code ? String(detail.status_code) : ''}
              />
              <MetaRow label={t('Model')} value={detail.model_name} />
              <MetaRow label={t('Channel')} value={channelValue} />
              <MetaRow label={t('Error Type')} value={detail.error_type} />
              <MetaRow label={t('Error Code')} value={detail.error_code} />
              <MetaRow
                label={t('Expires')}
                value={
                  detail.expires_at
                    ? formatTimestampToDate(detail.expires_at)
                    : t('Never')
                }
              />
            </div>

            <Tabs defaultValue='request_body' className='min-w-0'>
              <TabsList className='w-full sm:w-fit'>
                <TabsTrigger value='request_body'>
                  {t('Request Body')}
                </TabsTrigger>
                <TabsTrigger value='error_detail'>
                  {t('Error Detail')}
                </TabsTrigger>
              </TabsList>
              <TabsContent value='request_body' className='min-w-0'>
                <PayloadPanel
                  title={t('Request Body')}
                  value={requestBody}
                  emptyText={t('No request body captured')}
                />
              </TabsContent>
              <TabsContent value='error_detail' className='min-w-0'>
                <PayloadPanel
                  title={t('Error Detail')}
                  value={errorDetail}
                  emptyText={t('No error detail captured')}
                />
              </TabsContent>
            </Tabs>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
