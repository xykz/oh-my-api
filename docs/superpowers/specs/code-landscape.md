# Code Landscape Specification

## Overview
This document provides a comprehensive overview of the codebase landscape for the Lingma2 API project. It describes the architecture, code structure, and key components of the system.

## Architecture Diagram

```mermaid
graph TD
    subgraph Frontend
        FE[React App]
        FE -->|API calls| API_GATEWAY[/api]
    end

    subgraph APIGateway
        API_GATEWAY[/api] -->|Reverse Proxy| BACKEND[/v1]
        API_GATEWAY[/api] -->|Reverse Proxy| ADMIN[/admin]
        API_GATEWAY[/api] -->|Reverse Proxy| STATIC[/]
    end

    subgraph Backend
        subgraph Auth
            AUTH[Authentication]
            AUTH --> CREDENTIALS[Credential Management]
        end
        
        subgraph API
            CHAT_API[/v1/chat/completions]
            MODEL_API[/v1/models]
            CHAT_API --> CHAT_SERVICE[Chat Service]
            MODEL_API --> MODEL_SERVICE[Model Service]
        end
        
        subgraph Admin
            ADMIN_API[/admin/*]
            ADMIN_API --> ADMIN_SERVICE[Admin Service]
        end
        
        subgraph DataProcessing
            DATA_PROCESSING[Data Processing]
            DATA_PROCESSING --> DB[Database]
            DATA_PROCESSING --> CACHE[Cache]
        end
        
        AUTH --> DATA_PROCESSING
        CHAT_SERVICE --> DATA_PROCESSING
        MODEL_SERVICE --> DATA_PROCESSING
        ADMIN_SERVICE --> DATA_PROCESSING
    end

    subgraph Database
        DB[(SQLite)]
        DB -->|request_logs| REQUEST_LOGS[Request Logs]
        DB -->|model_mappings| MODEL_MAPPINGS[Model Mappings]
        DB -->|policy_rules| POLICY_RULES[Policy Rules]
        DB -->|canonical_execution_records| CANONICAL_EXECUTION[Canonical Execution]
        DB -->|settings| SETTINGS[Settings]
    end

    subgraph Services
        CREDENTIALS -->|OAuth| OAUTH[OAuth Service]
        CHAT_SERVICE -->|Stream| STREAMING[Streaming]
        MODEL_SERVICE -->|Model| MODEL_PROVIDER[Model Provider]
        ADMIN_SERVICE -->|Management| LOG_MANAGEMENT[Log Management]
        ADMIN_SERVICE -->|Management| POLICY_MANAGEMENT[Policy Management]
    end

    subgraph External
        OAUTH -->|External Auth| GOOGLE[Google]
        OAUTH -->|External Auth| GITHUB[GitHub]
        MODEL_PROVIDER -->|API| OPENAI[OpenAI]
        MODEL_PROVIDER -->|API| ANTHROPIC[Anthropic]
        LOG_MANAGEMENT -->|Export| EXPORT[CSV Export]
        POLICY_MANAGEMENT -->|Rules| RULE_ENGINE[Rule Engine]
    end

    STATIC -->|Serves| FRONTEND[Frontend Files]
    FRONTEND -->|UI| FE

    style Frontend fill:#f9f,stroke:#333
    style Backend fill:#9ff,stroke:#333
    style Database fill:#f99,stroke:#333
    style Services fill:#9f9,stroke:#333
    style External fill:#ff9,stroke:#333
    style APIGateway fill:#f66,stroke:#333
```

## Architecture Description

### Frontend
- Built with React and TypeScript
- Communicates with backend via API calls
- Served as static files from the backend
- Handles user interface and client-side logic

### API Gateway
- Handles routing of incoming requests
- Reverse proxies to appropriate backend services
- Manages static file serving for frontend
- Provides a single entry point for all API operations

### Backend
The backend is composed of several subcomponents:

#### Authentication
- Manages user authentication
- Handles credential management
- Uses OAuth for third-party authentication
- Stores credentials securely in the database

#### API Services
- **Chat Service**: Handles chat completions and streaming
- **Model Service**: Manages model listings and model routing
- Both services process requests and interact with the data processing layer

#### Admin Service
- Provides administrative endpoints
- Handles user management, settings, and monitoring
- Allows for log viewing and cleanup
- Manages policies and system settings

#### Data Processing
- Central data processing layer
- Handles interactions with the database
- Manages caching for performance
- Processes data for all services

### Database
The system uses SQLite with the following key tables:

- **request_logs**: Tracks all incoming requests with detailed metrics
- **model_mappings**: Manages model routing and rewriting
- **policy_rules**: Enforces policies on incoming requests
- **canonical_execution_records**: Stores complete execution history
- **settings**: Stores system configuration

### Services
- **OAuth Service**: Handles third-party authentication
- **Streaming**: Manages real-time streaming of chat responses
- **Model Provider**: Interfaces with external AI providers
- **Log Management**: Handles log viewing, cleanup, and export
- **Policy Management**: Enforces and manages policies

### External Integrations
- **Google/GitHub**: OAuth providers for authentication
- **OpenAI/Anthropic**: External AI providers for model services
- **CSV Export**: Allows exporting of logs and data
- **Rule Engine**: Processes policy rules and enforcement

## Code Structure

### Main Components
- **main.go**: Entry point for the application
- **internal/api**: API handlers and server implementation
- **internal/proxy**: Core proxy functionality and data structures
- **internal/db**: Database models, migrations, and storage logic
- **internal/config**: Configuration management

### Key Data Structures
- **CanonicalRequest**: Standardized request format across services
- **CredentialSnapshot**: Secure storage of credentials
- **ModelStatus**: Tracks model availability and metadata
- **SessionState**: Manages conversation sessions

### Key Services
- **SignatureEngine**: Handles request signing and verification
- **CredentialManager**: Manages credential lifecycle
- **SessionStore**: Maintains conversation state
- **ModelService**: Handles model routing and selection
- **BodyBuilder**: Constructs requests for upstream services

## Data Flow

1. **Incoming Request**: Frontend makes API call to backend
2. **Authentication**: Request is authenticated
3. **Policy Enforcement**: Request is checked against policies
4. **Model Routing**: Request is routed to appropriate model
5. **Upstream Request**: Request is sent to external AI provider
6. **Response Processing**: Response is processed and streamed back
7. **Logging**: All requests and responses are logged
8. **Session Update**: Conversation state is updated

## Business Processes

### Chat Processing
1. User sends chat request
2. Request is authenticated
3. Policy rules are applied
4. Model is selected and request is routed
5. Response is streamed back to user
6. Request/response is logged
7. Session state is updated

### Model Management
1. Admin requests model list
2. Model service fetches and caches model list
3. Models are displayed in UI
4. Model mappings can be configured
5. Mappings are used for model routing

### Policy Enforcement
1. Policy rules are defined in admin UI
2. Rules are stored in database
3. Rules are applied to incoming requests
4. Actions are taken based on rule matches

### Log Management
1. All requests are logged
2. Logs include detailed metrics and timing
3. Logs can be viewed in admin UI
4. Logs can be exported or cleaned up

### Credential Management
1. Credentials are obtained via OAuth
2. Credentials are securely stored
3. Credentials are used for upstream requests
4. Token expiration is monitored

## Security Considerations
- Credentials are stored securely
- Token expiration is handled with grace period
- All sensitive data is encrypted
- Policy rules can restrict access and actions
- Admin endpoints require special token

## Extensibility
- New models can be added through configuration
- New policies can be defined in admin UI
- New services can be integrated through plugins
- New logging formats can be supported through extensions

## Key Technologies
- Go: Primary programming language
- SQLite: Database for storing logs and settings
- React: Frontend framework
- TypeScript: Type safety in frontend
- Playwright: End-to-end testing
- Vite: Frontend build tool
- Prettier: Code formatting
- ESLint: Code linting