// useTags — tag management + per-trade tag assignment.

interface Tag {
  id: string
  tenant_id: string
  name: string
  color: string | null
  owner: 'system' | 'user'
}

export function useTags() {
  async function list(): Promise<Tag[]> {
    const res = await $fetch<{ tags: Tag[] }>('/v1/tags')
    return res.tags
  }

  async function create(name: string, color?: string): Promise<Tag> {
    return await $fetch<Tag>('/v1/tags', {
      method: 'POST',
      body: { name, color },
    })
  }

  async function remove(id: string): Promise<void> {
    await $fetch(`/v1/tags/${id}`, { method: 'DELETE' })
  }

  async function listForTrade(tradeId: string): Promise<string[]> {
    const res = await $fetch<{ tag_ids: string[] }>(`/v1/trades/${tradeId}/tags`)
    return res.tag_ids
  }

  async function setForTrade(tradeId: string, tagIds: string[]): Promise<void> {
    await $fetch(`/v1/trades/${tradeId}/tags`, {
      method: 'PUT',
      body: { tag_ids: tagIds },
    })
  }

  return { list, create, remove, listForTrade, setForTrade }
}