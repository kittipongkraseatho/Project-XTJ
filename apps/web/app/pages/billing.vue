<script setup lang="ts">
// /billing — Phase 4 plan comparison + current subscription + invoice history.
import { ref, onMounted, computed } from 'vue'
import { useBilling, type Plan, type SubscriptionDTO, type Payment } from '~/composables/useBilling'

definePageMeta({ name: 'billing' })

const { listPlans, currentSubscription, checkout, portal, cancel, listPayments } = useBilling()

const plans = ref<Plan[]>([])
const sub = ref<SubscriptionDTO | null>(null)
const payments = ref<Payment[]>([])
const loading = ref(true)
const errorMsg = ref<string | null>(null)
const busyPlanCode = ref<string | null>(null)

async function refresh() {
  loading.value = true
  try {
    const [pl, sb, pp] = await Promise.all([
      listPlans(),
      currentSubscription().catch(() => null),
      listPayments().catch(() => ({ payments: [] })),
    ])
    plans.value = pl.plans
    sub.value = sb
    payments.value = pp.payments
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? err?.message ?? 'Failed to load billing'
  } finally {
    loading.value = false
  }
}

async function onUpgrade(p: Plan) {
  busyPlanCode.value = p.code
  try {
    const origin = window.location.origin
    const res = await checkout({
      plan_code: p.code,
      success_url: `${origin}/billing?status=success`,
      cancel_url: `${origin}/billing?status=cancel`,
    })
    // Redirect to the (mock or real Stripe) URL.
    window.location.href = res.checkout_url
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? 'Checkout failed'
  } finally {
    busyPlanCode.value = null
  }
}

async function onOpenPortal() {
  try {
    const res = await portal()
    window.location.href = res.portal_url
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? 'Portal failed'
  }
}

async function onCancel() {
  if (!confirm('Cancel subscription at end of current period?')) return
  try {
    await cancel(true)
    await refresh()
  } catch (err: any) {
    errorMsg.value = err?.data?.error?.message ?? 'Cancel failed'
  }
}

function fmtMoney(cents: number, currency: string) {
  return (cents / 100).toLocaleString('en-US', { style: 'currency', currency: currency || 'USD' })
}

onMounted(refresh)

const currentPlan = computed(() => plans.value.find((p) => p.code === sub.value?.plan_code))
</script>

<template>
  <v-container class="py-6">
    <h1 class="text-h4 mb-4">Billing</h1>

    <v-progress-circular v-if="loading" indeterminate color="primary" />

    <v-alert v-else-if="errorMsg" type="error" :text="errorMsg" />

    <template v-else>
      <!-- Current subscription -->
      <v-card v-if="sub" variant="outlined" class="pa-4 mb-4">
        <h2 class="text-h6 mb-2">Current Plan</h2>
        <div class="d-flex align-center justify-space-between">
          <div>
            <div class="text-h5 font-weight-bold">{{ sub.plan_name }}</div>
            <div class="text-body-2 text-medium-emphasis">
              {{ fmtMoney(currentPlan?.price_cents ?? 0, currentPlan?.currency ?? 'USD') }}
              / {{ currentPlan?.interval ?? 'month' }} · Status: <strong>{{ sub.status }}</strong>
            </div>
            <div class="text-caption text-medium-emphasis mt-1">
              Period: {{ sub.current_period_start }} → {{ sub.current_period_end }}
              <span v-if="sub.cancel_at_period_end" class="text-warning ml-2">
                · cancels at {{ sub.cancel_at }}
              </span>
            </div>
          </div>
          <div>
            <v-btn variant="outlined" prepend-icon="mdi-account-cog" @click="onOpenPortal">
              Manage payment
            </v-btn>
            <v-btn
              v-if="!sub.cancel_at_period_end"
              variant="text"
              color="error"
              class="ml-2"
              @click="onCancel"
            >
              Cancel
            </v-btn>
          </div>
        </div>
      </v-card>

      <v-card v-else variant="outlined" class="pa-4 mb-4 text-center">
        <div class="text-body-2 text-medium-emphasis">
          No active subscription. Pick a plan below to get started.
        </div>
      </v-card>

      <!-- Plan comparison -->
      <h2 class="text-h5 mb-3">Plans</h2>
      <v-row>
        <v-col v-for="p in plans" :key="p.code" cols="12" sm="6" md="3">
          <v-card variant="outlined" class="pa-4 text-center">
            <div class="text-h6 mb-1">{{ p.name }}</div>
            <div class="text-h4 font-weight-bold">
              {{ fmtMoney(p.price_cents, p.currency) }}
            </div>
            <div class="text-caption text-medium-emphasis mb-3">
              per {{ p.interval }}
            </div>
            <div class="text-body-2 mb-3 text-medium-emphasis">
              {{ p.description || '—' }}
            </div>
            <v-btn
              color="primary"
              variant="elevated"
              :loading="busyPlanCode === p.code"
              :disabled="sub?.plan_code === p.code"
              @click="onUpgrade(p)"
            >
              {{ sub?.plan_code === p.code ? 'Current' : 'Choose' }}
            </v-btn>
          </v-card>
        </v-col>
      </v-row>

      <!-- Invoice history -->
      <v-card v-if="payments.length" variant="outlined" class="pa-4 mt-6">
        <h2 class="text-h6 mb-3">Invoice History</h2>
        <v-table density="compact">
          <thead>
            <tr>
              <th>Date</th>
              <th class="text-right">Amount</th>
              <th>Status</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="p in payments" :key="p.id">
              <td class="text-caption">
                {{ new Date(p.paid_at ?? p.created_at).toLocaleDateString() }}
              </td>
              <td class="text-right font-weight-medium">
                {{ fmtMoney(p.amount_cents, p.currency) }}
              </td>
              <td>
                <v-chip
                  size="x-small"
                  :color="p.status === 'paid' ? 'success' : p.status === 'failed' ? 'error' : 'grey'"
                >
                  {{ p.status }}
                </v-chip>
              </td>
            </tr>
          </tbody>
        </v-table>
      </v-card>
    </template>
  </v-container>
</template>