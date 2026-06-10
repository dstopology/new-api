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
import { useEffect, useRef, useState } from 'react'
import { Bell, Megaphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getAnnouncementColorClass } from '@/lib/colors'
import { formatDateTimeObject } from '@/lib/time'
import { cn } from '@/lib/utils'
import { useNotifications } from '@/hooks/use-notifications'
import { useNotificationStore } from '@/stores/notification-store'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Markdown } from '@/components/ui/markdown'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

type AnnouncementItem = {
  type?: string
  content?: string
  extra?: string
  publishDate?: string | Date
}

function AnnouncementDot({ type }: { type?: string }) {
  return (
    <span
      className={cn(
        'mt-1.5 inline-block size-2 shrink-0 rounded-full',
        getAnnouncementColorClass(type)
      )}
    />
  )
}

function formatAnnouncementTime(publishDate?: string | Date) {
  if (!publishDate) return ''

  const date = new Date(publishDate)
  if (Number.isNaN(date.getTime())) {
    return typeof publishDate === 'string' ? publishDate : ''
  }

  return formatDateTimeObject(date)
}

export function NotificationDialog() {
  const { t } = useTranslation()
  const {
    activeTab,
    announcements,
    loading,
    notice,
    setActiveTab,
    unreadAnnouncementsCount,
  } = useNotifications()
  const isNoticeClosed = useNotificationStore((state) => state.isNoticeClosed)
  const markNoticeRead = useNotificationStore((state) => state.markNoticeRead)
  const setClosedUntilDate = useNotificationStore(
    (state) => state.setClosedUntilDate
  )
  const [open, setOpen] = useState(false)
  const autoOpenedNoticeRef = useRef('')

  useEffect(() => {
    if (loading || !notice || isNoticeClosed()) return
    if (autoOpenedNoticeRef.current === notice) return

    const timer = window.setTimeout(() => {
      autoOpenedNoticeRef.current = notice
      setActiveTab('notice')
      setOpen(true)
    }, 0)

    return () => window.clearTimeout(timer)
  }, [isNoticeClosed, loading, notice, setActiveTab])

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && notice) {
      markNoticeRead(notice)
    }
    setOpen(nextOpen)
  }

  const handleCloseToday = () => {
    setClosedUntilDate(new Date().toDateString())
    if (notice) {
      markNoticeRead(notice)
    }
    setOpen(false)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-hidden p-0 sm:max-w-2xl'>
        <Tabs
          value={activeTab}
          onValueChange={setActiveTab as (value: string) => void}
          className='min-h-0'
        >
          <DialogHeader className='border-b px-5 pt-5 pb-3'>
            <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
              <DialogTitle className='flex items-center gap-2 text-lg'>
                <Bell className='size-5' />
                {t('System Announcements')}
              </DialogTitle>
              <TabsList className='grid w-full grid-cols-2 sm:w-auto'>
                <TabsTrigger value='notice' className='gap-1.5'>
                  <Bell className='size-3.5' />
                  {t('Notice')}
                </TabsTrigger>
                <TabsTrigger value='announcements' className='gap-1.5'>
                  <Megaphone className='size-3.5' />
                  {t('Timeline')}
                  {unreadAnnouncementsCount > 0 ? (
                    <span className='bg-primary text-primary-foreground ms-1 rounded-full px-1.5 py-0.5 text-[10px] leading-none'>
                      {unreadAnnouncementsCount > 99
                        ? '99+'
                        : unreadAnnouncementsCount}
                    </span>
                  ) : null}
                </TabsTrigger>
              </TabsList>
            </div>
          </DialogHeader>

          <TabsContent value='notice' className='m-0'>
            <ScrollArea className='max-h-[60vh] px-5 py-4'>
              {loading ? (
                <p className='text-muted-foreground py-10 text-center text-sm'>
                  {t('Loading...')}
                </p>
              ) : notice ? (
                <Markdown>{notice}</Markdown>
              ) : (
                <p className='text-muted-foreground py-10 text-center text-sm'>
                  {t('No announcements at this time')}
                </p>
              )}
            </ScrollArea>
          </TabsContent>

          <TabsContent value='announcements' className='m-0'>
            <ScrollArea className='max-h-[60vh] px-5 py-4'>
              {announcements.length === 0 ? (
                <p className='text-muted-foreground py-10 text-center text-sm'>
                  {t('No system announcements')}
                </p>
              ) : (
                <div className='flex flex-col'>
                  {(announcements as AnnouncementItem[]).map((item, idx) => {
                    const time = formatAnnouncementTime(item.publishDate)

                    return (
                      <div key={`${time}-${idx}`}>
                        <div className='py-3'>
                          <div className='flex items-start gap-3'>
                            <AnnouncementDot type={item.type} />
                            <div className='min-w-0 flex-1 space-y-2'>
                              {item.content ? (
                                <Markdown>{item.content}</Markdown>
                              ) : null}
                              {item.extra ? (
                                <Markdown className='text-muted-foreground text-xs'>
                                  {item.extra}
                                </Markdown>
                              ) : null}
                              {time ? (
                                <p className='text-muted-foreground text-xs'>
                                  {time}
                                </p>
                              ) : null}
                            </div>
                          </div>
                        </div>
                        {idx < announcements.length - 1 ? <Separator /> : null}
                      </div>
                    )
                  })}
                </div>
              )}
            </ScrollArea>
          </TabsContent>
        </Tabs>

        <DialogFooter className='bg-muted/30 border-t px-5 py-4'>
          <Button variant='outline' onClick={handleCloseToday}>
            {t('Close Today')}
          </Button>
          <Button onClick={() => handleOpenChange(false)}>{t('Close')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
