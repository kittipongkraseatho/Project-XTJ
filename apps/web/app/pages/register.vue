<script setup lang="ts">
import { ref } from 'vue'
import { useAuth } from '~/composables/useAuth'
import OAuthButtonGroup from '~/components/OAuthButtonGroup.vue'

definePageMeta({ name: 'register' })

const { register } = useAuth()
const email = ref('')
const password = ref('')
const displayName = ref('')
const loading = ref(false)
const errorMsg = ref<string | null>(null)
const errorCode = ref<string | null>(null)
const router = useRouter()
const route = useRoute()

async function onSubmit() {
  errorMsg.value = null
  errorCode.value = null
  if (!email.value || !password.value || !displayName.value) {
    errorCode.value = 'VALIDATION_FAILED'
    errorMsg.value = 'Please fill all fields.'
    return
  }
  loading.value = true
  try {
    await register({ email: email.value, password: password.value, display_name: displayName.value })
    const redirect = (route.query.redirect as string) || '/dashboard'
    await router.push(redirect)
  } catch (err: any) {
    errorCode.value = err?.code ?? 'UNKNOWN'
    errorMsg.value = err?.message ?? 'Registration failed'
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
          <h1 class="text-h5 mb-1">Create your account</h1>
          <p class="text-body-2 text-medium-emphasis mb-6">Free plan · no credit card required.</p>

          <v-form @submit.prevent="onSubmit">
            <v-text-field v-model="displayName" label="Display name" autocomplete="name" required class="mb-3" />
            <v-text-field v-model="email" label="Email" type="email" autocomplete="email" required class="mb-3" />
            <v-text-field v-model="password" label="Password" type="password"
                          autocomplete="new-password" hint="Min 8 chars, letters + digits"
                          persistent-hint required class="mb-4" />
            <v-alert v-if="errorMsg" type="error" variant="tonal" class="mb-4" :text="`${errorCode ?? ''}: ${errorMsg}`" />
            <v-btn type="submit" color="primary" block :loading="loading">Sign up</v-btn>
          </v-form>

          <v-divider class="my-6">or</v-divider>
          <OAuthButtonGroup />

          <div class="text-body-2 text-medium-emphasis text-center mt-4">
            Already have an account? <NuxtLink to="/login">Sign in</NuxtLink>
          </div>
        </v-card>
      </v-col>
    </v-row>
  </v-container>
</template>