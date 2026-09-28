/**
 * User Announcements API endpoints
 */

import { apiClient } from './client'
import type { UserAnnouncement } from '@/types'

export async function list(unreadOnly: boolean = false): Promise<UserAnnouncement[]> {
  return (await listSnapshot(unreadOnly)).items
}

export async function listSnapshot(unreadOnly: boolean = false): Promise<{
  items: UserAnnouncement[]
  fetchedAt: number
}> {
  const { data, headers } = await apiClient.get<UserAnnouncement[]>('/announcements', {
    params: unreadOnly ? { unread_only: 1 } : {}
  })
  const serverTime = Date.parse(headers.date ?? '')
  return { items: data, fetchedAt: Number.isFinite(serverTime) ? serverTime : Date.now() }
}

export async function markRead(id: number): Promise<{ message: string }> {
  const { data } = await apiClient.post<{ message: string }>(`/announcements/${id}/read`)
  return data
}

const announcementsAPI = {
  list,
  listSnapshot,
  markRead
}

export default announcementsAPI

