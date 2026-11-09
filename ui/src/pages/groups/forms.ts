// Eingaben für Gruppe und Zeitplan: Ausgangswerte, Prüfung und Körper für die API.
// Das Backend prüft alles noch einmal; hier geht es um frühes, verständliches Feedback.

import {
  DEFAULT_PRIORITY,
  MAX_PRIORITY,
  MAX_SPREAD_MINUTES,
  type Group,
  type GroupBody,
  type Schedule,
  type ScheduleBody,
} from '../../api/groups';
import { parseCron } from '../../components/cron';
import { LIMITS, charCount, hasControlChars } from '../../lib/validate';

export interface GroupForm {
  name: string;
  description: string;
  priority: string;
}

export function emptyGroupForm(): GroupForm {
  return { name: '', description: '', priority: String(DEFAULT_PRIORITY) };
}

export function groupFormOf(group: Group): GroupForm {
  return { name: group.name, description: group.description, priority: String(group.priority) };
}

export interface GroupFormErrors {
  name?: string;
  description?: string;
  priority?: string;
}

function wholeNumber(text: string): number | null {
  return /^\d+$/.test(text.trim()) ? Number(text.trim()) : null;
}

export function validateGroupForm(form: GroupForm): GroupFormErrors {
  const errors: GroupFormErrors = {};
  const name = form.name.trim();
  if (name === '') errors.name = 'groups.form.nameRequired';
  else if (charCount(name) > LIMITS.name) errors.name = 'groups.form.nameTooLong';
  else if (hasControlChars(name)) errors.name = 'check.oneLine';
  if (charCount(form.description.trim()) > LIMITS.description) errors.description = 'groups.form.descriptionTooLong';
  else if (hasControlChars(form.description, true)) errors.description = 'check.controlChars';
  const priority = wholeNumber(form.priority);
  if (priority === null || priority < 1 || priority > MAX_PRIORITY) errors.priority = 'groups.form.priorityRange';
  return errors;
}

export function groupBody(form: GroupForm): GroupBody {
  return { name: form.name.trim(), description: form.description.trim(), priority: wholeNumber(form.priority) ?? DEFAULT_PRIORITY };
}

export interface ScheduleForm {
  name: string;
  cron_expr: string;
  spread_minutes: string;
}

export function emptyScheduleForm(): ScheduleForm {
  return { name: '', cron_expr: '0 3 * * *', spread_minutes: '0' };
}

export function scheduleFormOf(schedule: Schedule): ScheduleForm {
  return { name: schedule.name, cron_expr: schedule.cron_expr, spread_minutes: String(schedule.spread_minutes) };
}

export interface ScheduleFormErrors {
  name?: string;
  cron?: string;
  spread?: string;
}

export function validateScheduleForm(form: ScheduleForm): ScheduleFormErrors {
  const errors: ScheduleFormErrors = {};
  const name = form.name.trim();
  if (name === '') errors.name = 'schedules.form.nameRequired';
  else if (charCount(name) > LIMITS.name) errors.name = 'schedules.form.nameTooLong';
  else if (hasControlChars(name)) errors.name = 'check.oneLine';
  if (parseCron(form.cron_expr) === null || charCount(form.cron_expr.trim()) > LIMITS.cron) errors.cron = 'schedules.form.cronInvalid';
  const spread = wholeNumber(form.spread_minutes);
  if (spread === null || spread > MAX_SPREAD_MINUTES) errors.spread = 'schedules.form.spreadRange';
  return errors;
}

export function scheduleBody(form: ScheduleForm): ScheduleBody {
  return { name: form.name.trim(), cron_expr: form.cron_expr.trim().split(/\s+/).join(' '), spread_minutes: wholeNumber(form.spread_minutes) ?? 0 };
}

export function isFormValid(errors: object): boolean {
  return Object.keys(errors).length === 0;
}

export function sameForm<T extends object>(a: T, b: T): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}
