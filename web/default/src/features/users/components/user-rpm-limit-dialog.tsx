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
import { type FormEvent, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Field,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { getGroups, updateUserRpmLimit } from '../api'
import type { User } from '../types'

const MAX_USER_RPM_LIMIT = 100_000_000

type UserRpmLimitDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  user: User
  onSuccess: () => void
}

export function UserRpmLimitDialog({
  open,
  onOpenChange,
  user,
  onSuccess,
}: UserRpmLimitDialogProps) {
  const { t } = useTranslation()
  const initialGroup = Object.keys(user.rpm_limits ?? {})[0] || user.group
  const [group, setGroup] = useState(initialGroup)
  const [value, setValue] = useState(
    String(user.rpm_limits?.[initialGroup] ?? 0)
  )
  const [touched, setTouched] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)

  const { data: groupsData } = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    staleTime: 5 * 60 * 1000,
  })
  const groups = useMemo(
    () =>
      Array.from(
        new Set([
          user.group,
          'auto',
          ...Object.keys(user.rpm_limits ?? {}),
          ...(groupsData?.data ?? []),
        ])
      ).filter(Boolean),
    [groupsData?.data, user.group, user.rpm_limits]
  )

  const parsedValue = Number(value)
  const isValid =
    group.trim() !== '' &&
    value.trim() !== '' &&
    Number.isInteger(parsedValue) &&
    parsedValue >= 0 &&
    parsedValue <= MAX_USER_RPM_LIMIT
  const errorMessage =
    touched && !isValid
      ? t('RPM limit must be an integer between 0 and 100000000')
      : undefined

  const handleOpenChange = (nextOpen: boolean) => {
    if (!isSubmitting) {
      onOpenChange(nextOpen)
    }
  }

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setTouched(true)
    if (!isValid) return

    setIsSubmitting(true)
    try {
      const result = await updateUserRpmLimit(user.id, group, parsedValue)
      if (!result.success) {
        toast.error(result.message || t('Failed to update RPM limit'))
        return
      }

      toast.success(t('RPM limit updated'))
      onOpenChange(false)
      onSuccess()
    } catch (_error) {
      toast.error(t('Failed to update RPM limit'))
    } finally {
      setIsSubmitting(false)
    }
  }

  const groupInputId = `user-rpm-limit-group-${user.id}`
  const limitInputId = `user-rpm-limit-value-${user.id}`

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('Set group RPM limit')}</DialogTitle>
          <DialogDescription>
            {t(
              'Set a requests-per-minute limit for {{username}} in a specific group.',
              { username: user.username }
            )}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor={groupInputId}>{t('Group')}</FieldLabel>
              <Select
                items={groups.map((item) => ({ value: item, label: item }))}
                value={group}
                onValueChange={(nextGroup) => {
                  if (nextGroup === null) return
                  setGroup(nextGroup)
                  setValue(String(user.rpm_limits?.[nextGroup] ?? 0))
                  setTouched(false)
                }}
              >
                <SelectTrigger id={groupInputId}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {groups.map((item) => (
                      <SelectItem key={item} value={item}>
                        {item}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>

            <Field data-invalid={errorMessage ? true : undefined}>
              <FieldLabel htmlFor={limitInputId}>{t('RPM limit')}</FieldLabel>
              <Input
                id={limitInputId}
                type='number'
                min={0}
                max={MAX_USER_RPM_LIMIT}
                step={1}
                inputMode='numeric'
                value={value}
                onChange={(event) => setValue(event.target.value)}
                onBlur={() => setTouched(true)}
                aria-invalid={errorMessage ? true : undefined}
                autoFocus
              />
              <FieldDescription>
                {t('0 removes the limit for the selected user and group.')}
              </FieldDescription>
              <FieldError>{errorMessage}</FieldError>
            </Field>
          </FieldGroup>

          <DialogFooter className='mt-5'>
            <Button
              type='button'
              variant='outline'
              onClick={() => handleOpenChange(false)}
              disabled={isSubmitting}
            >
              {t('Cancel')}
            </Button>
            <Button type='submit' disabled={isSubmitting}>
              {isSubmitting && <Spinner data-icon='inline-start' />}
              {isSubmitting ? t('Saving...') : t('Save changes')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
