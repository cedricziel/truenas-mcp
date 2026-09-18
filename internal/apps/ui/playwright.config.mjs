import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './test',
  fullyParallel: true,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    browserName: 'chromium',
  },
});
