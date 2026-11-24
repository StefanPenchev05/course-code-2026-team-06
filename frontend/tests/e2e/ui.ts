import { expect, type Page } from '@playwright/test';

/** Browser steps shared by several journeys. */

export async function logIn(page: Page, email: string, password: string) {
  await page.getByLabel('University email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Log in' }).click();
}

export async function logOut(page: Page) {
  await page.getByRole('button', { name: 'Log out' }).click();
  await expect(page.getByRole('heading', { name: 'Log in' })).toBeVisible();
}
