<script setup lang="ts">
import { ref } from 'vue'
import { useAuth } from '~/composables/useAuth'
import OAuthButtonGroup from '~/components/OAuthButtonGroup.vue'

definePageMeta({ name: 'login' })

const { login } = useAuth()
const email = ref('')
const password = ref('')
const loading = ref(false)
const errorMsg = ref<string | null>(null)
const errorCode = ref<string | null>(null)
const router = useRouter()
const route = useRoute()

const oauthError = computed(() => route.query.error as string | undefined)
const oauthProvider = computed(() => route.query.provider as string | undefined)

async function onSubmit() {
  errorMsg.value = null
  errorCode.value = null
  loading.value = true
  try {
    await login({ email: email.value, password: password.value })
    const redirect = (route.query.redirect as string) || '/dashboard'
    await router.push(redirect)
  } catch (err: any) {
    errorCode.value = err?.code ?? 'UNKNOWN'
    errorMsg.value = err?.message ?? 'Login failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <v-container class="py-12">
    <v-row justify="center">
      <v-col cols="12" sm="8" md="6" lg="4">
        <v-card class="pa-6">
          <h1 class="text-h5 mb-1">Welcome back</h1>
          <p class="text-body-2 text-medium-emphasis mb-6">Sign in to your Trading Journal.</p>

          <v-alert v-if="oauthError" type="warning" variant="tonal" class="mb-4">
            OAuth sign-in failed: {{ oauthError }} {{ oauthProvider ? `(${oauthProvider})` : '' }}
          </v-alert>

          <v-form @submit.prevent="onSubmit">
            <v-text-field v-model="email" label="Email" type="email" autocomplete="email" required class="mb-3" />
            <v-text-field v-model="password" label="Password" type="password"
                          autocomplete="current-password" required class="mb-4" />
            <v-alert v-if="errorMsg" type="error" variant="tonal" class="mb-4" :text="`${errorCode ?? ''}: ${errorMsg}`" />
            <v-btn type="submit" color="primary" block :loading="loading">Sign in</v-btn>
          </v-form>

          <v-divider class="my-6">or</v-divider>
          <OAuthButtonGroup />

          <div class="text-body-2 text-medium-emphasis text-center mt-4">
            No account yet? <NuxtLink to="/register">Sign up</NuxtLink>
          </div>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>