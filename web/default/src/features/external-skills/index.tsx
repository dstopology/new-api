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
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, Puzzle, Search, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'
import { formatTimestampToDate } from '@/lib/format'
import { SectionPageLayout } from '@/components/layout'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import {
  createExternalSkill,
  deleteExternalSkill,
  getExternalSkill,
  listExternalSkills,
  updateExternalSkill,
} from './api'
import type { ExternalSkill, ExternalSkillInput } from './types'

const PAGE_SIZE = 20

const externalSkillSchema = z.object({
  name: z.string().trim().min(1, 'Skill name is required').max(128),
  description: z.string().trim(),
  content: z
    .string()
    .refine((value) => value.trim().length > 0, 'Skill content is required'),
})

type ExternalSkillFormValues = z.infer<typeof externalSkillSchema>

const emptyForm: ExternalSkillFormValues = {
  name: '',
  description: '',
  content: '',
}

export function ExternalSkills() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [page, setPage] = useState(1)
  const [keyword, setKeyword] = useState('')
  const [editorOpen, setEditorOpen] = useState(false)
  const [editingSkill, setEditingSkill] = useState<ExternalSkill | null>(null)
  const [deletingSkill, setDeletingSkill] = useState<ExternalSkill | null>(null)

  const query = useQuery({
    queryKey: ['external-skills', page, keyword],
    queryFn: () => listExternalSkills({ page, pageSize: PAGE_SIZE, keyword }),
  })
  const skills = query.data?.data?.items || []
  const total = query.data?.data?.total || 0
  const pageCount = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const openCreate = () => {
    setEditingSkill(null)
    setEditorOpen(true)
  }

  const openEdit = async (skill: ExternalSkill) => {
    const result = await getExternalSkill(skill.id)
    setEditingSkill(result.data || skill)
    setEditorOpen(true)
  }

  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteExternalSkill(id),
    onSuccess: (result) => {
      if (!result.success) return
      toast.success(t('External skill deleted'))
      setDeletingSkill(null)
      if (skills.length === 1 && page > 1) setPage(page - 1)
      queryClient.invalidateQueries({ queryKey: ['external-skills'] })
    },
  })

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('External Skills')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button size='sm' onClick={openCreate}>
            <Plus data-icon='inline-start' />
            {t('Add Skill')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='flex flex-col gap-4'>
            <div className='relative max-w-sm'>
              <Search className='text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2' />
              <Input
                value={keyword}
                onChange={(event) => {
                  setKeyword(event.target.value)
                  setPage(1)
                }}
                placeholder={t('Search external skills...')}
                className='pl-9'
              />
            </div>

            {query.isLoading ? (
              <div className='flex flex-col gap-2'>
                {Array.from({ length: 5 }).map((_, index) => (
                  <Skeleton key={index} className='h-12 w-full' />
                ))}
              </div>
            ) : skills.length === 0 ? (
              <Empty className='min-h-64 border'>
                <EmptyHeader>
                  <EmptyMedia variant='icon'>
                    <Puzzle />
                  </EmptyMedia>
                  <EmptyTitle>{t('No external skills found')}</EmptyTitle>
                  <EmptyDescription>
                    {t('Add a skill to make it available from the public API.')}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <div className='overflow-hidden rounded-md border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Name')}</TableHead>
                      <TableHead>{t('Description')}</TableHead>
                      <TableHead className='hidden lg:table-cell'>
                        {t('Updated')}
                      </TableHead>
                      <TableHead className='w-24 text-right'>
                        {t('Actions')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {skills.map((skill) => (
                      <TableRow key={skill.id}>
                        <TableCell className='max-w-48 truncate font-medium'>
                          {skill.name}
                        </TableCell>
                        <TableCell className='text-muted-foreground max-w-xl truncate whitespace-normal'>
                          {skill.description || '-'}
                        </TableCell>
                        <TableCell className='hidden font-mono lg:table-cell'>
                          {formatTimestampToDate(skill.updated_time)}
                        </TableCell>
                        <TableCell>
                          <div className='flex justify-end gap-1'>
                            <Button
                              variant='ghost'
                              size='icon-sm'
                              onClick={() => openEdit(skill)}
                              aria-label={t('Edit skill')}
                              title={t('Edit skill')}
                            >
                              <Pencil />
                            </Button>
                            <Button
                              variant='ghost'
                              size='icon-sm'
                              onClick={() => setDeletingSkill(skill)}
                              aria-label={t('Delete skill')}
                              title={t('Delete skill')}
                            >
                              <Trash2 />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}

            <div className='flex items-center justify-end gap-2'>
              <span className='text-muted-foreground text-sm'>
                {t('Page {{page}} of {{total}}', {
                  page,
                  total: pageCount,
                })}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={page <= 1}
                onClick={() => setPage((value) => value - 1)}
              >
                {t('Previous')}
              </Button>
              <Button
                variant='outline'
                size='sm'
                disabled={page >= pageCount}
                onClick={() => setPage((value) => value + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <ExternalSkillEditor
        open={editorOpen}
        onOpenChange={setEditorOpen}
        skill={editingSkill}
      />

      <AlertDialog
        open={deletingSkill !== null}
        onOpenChange={(open) => !open && setDeletingSkill(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete external skill?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('This will permanently delete {{name}}.', {
                name: deletingSkill?.name || '',
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={deleteMutation.isPending}
              onClick={() =>
                deletingSkill && deleteMutation.mutate(deletingSkill.id)
              }
              className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
            >
              {deleteMutation.isPending ? t('Deleting...') : t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function ExternalSkillEditor({
  open,
  onOpenChange,
  skill,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  skill: ExternalSkill | null
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<ExternalSkillFormValues>({
    resolver: zodResolver(externalSkillSchema),
    defaultValues: emptyForm,
  })

  useEffect(() => {
    if (!open) return
    form.reset(
      skill
        ? {
            name: skill.name,
            description: skill.description,
            content: skill.content,
          }
        : emptyForm
    )
  }, [form, open, skill])

  const mutation = useMutation({
    mutationFn: (values: ExternalSkillInput) =>
      skill
        ? updateExternalSkill(skill.id, values)
        : createExternalSkill(values),
    onSuccess: (result) => {
      if (!result.success) return
      toast.success(
        skill ? t('External skill updated') : t('External skill created')
      )
      queryClient.invalidateQueries({ queryKey: ['external-skills'] })
      onOpenChange(false)
    },
  })

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[640px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {skill ? t('Edit External Skill') : t('Add External Skill')}
          </SheetTitle>
          <SheetDescription>
            {t(
              'Configure the name, description, and content returned by the public API.'
            )}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='external-skill-form'
            onSubmit={form.handleSubmit((values) => mutation.mutate(values))}
            className={sideDrawerFormClassName()}
          >
            <SideDrawerSection>
              <FormField
                control={form.control}
                name='name'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Name')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder='ultracode' />
                    </FormControl>
                    <FormDescription>
                      {t('Used in the public skill URL.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='description'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Description')}</FormLabel>
                    <FormControl>
                      <Textarea
                        {...field}
                        rows={3}
                        placeholder={t(
                          'Describe when this skill should be used'
                        )}
                      />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
              <FormField
                control={form.control}
                name='content'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Content')}</FormLabel>
                    <FormControl>
                      <Textarea
                        {...field}
                        rows={16}
                        className='font-mono'
                        placeholder={t('Enter the skill instructions')}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Returned as JSON or plain text from the public API.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose
            render={<Button variant='outline' disabled={mutation.isPending} />}
          >
            {t('Cancel')}
          </SheetClose>
          <Button
            type='submit'
            form='external-skill-form'
            disabled={mutation.isPending}
          >
            {mutation.isPending ? t('Saving...') : t('Save')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
