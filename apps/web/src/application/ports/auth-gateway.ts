import type { User } from '../../domain/types.ts'

export interface AuthGateway {
  login(username: string, password: string): Promise<User>
  logout(): Promise<void>
  currentSession(): Promise<User | null>
  listUsers(): Promise<User[]>
}
